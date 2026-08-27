// The authorized sink this module's suite drives a tool through.
//
// Everything above this file answers the question "who is asking". This one
// answers the question that follows it: what happens to the statement when the
// answer is nobody. A tool asks a service, the service asks a policy, the
// policy issues the Grant, and only a Grant reaches the handle -- which is the
// same path a controller takes, and the reason this package has no second
// enforcement point.
//
// The handle is the instrumented one a module constructor is given, reached
// through the five verbs a model connection answers: Select, Insert, Update,
// Delete and Statement. Under it is a driver that counts what arrived, so the
// number of statements is read from two places that cannot both be wrong in the
// same direction -- the collector, which the handle writes to, and the driver,
// which is where a statement ends up whether anything recorded it or not.

package helpers

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/arandu-io/framework/data"
	"github.com/arandu-io/framework/security"

	"github.com/arandu-io/mcp"
)

// SectionList is the action the policy below is asked about.
const SectionList security.Action = "section.list"

// EditorRole is what SectionPolicy looks for. A subject without it is refused.
const EditorRole = "editor"

// Section is the resource the policy decides about. It is a plain struct rather
// than anything with behaviour, because the only thing this suite asks of it is
// to be the type parameter a policy is written for.
type Section struct {
	ID   string
	Name string
}

// SectionPolicy allows an editor and refuses everybody else, including a guest.
//
// It is the whole of the authorization in this suite, and it is deliberately
// one rule: a policy with branches would let a test pass because it took a
// branch nobody meant, and what is being proved here is not the rule but where
// it runs relative to the statement.
type SectionPolicy struct{}

// Can answers whether the subject may perform the action on the section.
func (SectionPolicy) Can(_ context.Context, s security.Subject, a security.Action, _ Section) error {
	for _, role := range s.Roles {
		if role == EditorRole {
			return nil
		}
	}
	return fmt.Errorf("%s needs the %s role", a, EditorRole)
}

// SectionService is where the statement about sections is written.
//
// It holds the handle and nothing else, and every method on it asks the policy
// before it reads the handle. The tenant is never a parameter: it is taken off
// the Grant the policy produced, so a caller has no way to name one.
type SectionService struct{ db *data.DB }

// NewSectionService returns a service over the given handle.
func NewSectionService(db *data.DB) *SectionService { return &SectionService{db: db} }

// List returns the names of the sections the subject may see.
//
// The order is the point of it: Authorize first, and the handle only after a
// Grant exists. A refusal returns before the handle is touched at all, so there
// is no statement to filter and no result to discard -- which is the difference
// between a refusal and an empty page.
func (s *SectionService) List(ctx context.Context, sub security.Subject) ([]string, error) {
	g, err := security.Authorize(ctx, SectionPolicy{}, sub, SectionList, Section{})
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Select(ctx,
		"select id, name from sections where tenant_id = ?",
		[]any{security.Tenant(g)}, false)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(rows))
	for _, row := range rows {
		name, _ := row["name"].(string)
		names = append(names, name)
	}
	return names, nil
}

// Sections is a tool over SectionService.
//
// It reads no identity of its own: the subject is the one the transport put on
// the request, handed straight to the service. A tool that could choose a
// subject is a tool that chooses its own permissions.
type Sections struct{ svc *SectionService }

// NewSections returns the tool over the given service.
func NewSections(svc *SectionService) *Sections { return &Sections{svc: svc} }

// Name and Description are what a client lists the tool as.
func (*Sections) Name() string        { return "list_sections" }
func (*Sections) Description() string { return "Lists the sections of this blog." }

// Schema declares no arguments: what the tool returns is decided by who is
// asking, and there is nothing for the model to fill in.
func (*Sections) Schema() mcp.Schema { return mcp.Object() }

// Handle asks the service as the subject the request carried.
func (t *Sections) Handle(ctx context.Context, r mcp.Request) (mcp.Response, error) {
	names, err := t.svc.List(ctx, r.Subject())
	if err != nil {
		return mcp.Response{}, err
	}
	return mcp.JSON(names), nil
}

// SectionsServer is a server carrying the one tool, over the given handle.
func SectionsServer(db *data.DB) *mcp.Server {
	return &mcp.Server{
		Name: "blog", Version: "1.0.0",
		Instructions: "The sections of a blog.",
		Tools:        []mcp.Tool{NewSections(NewSectionService(db))},
	}
}

// CountingHandle returns an instrumented handle over a driver that counts the
// statements that reached it, and the counter.
//
// The counter is the independent witness. The handle records every statement on
// the collector, so a test could read the count from there alone -- and would
// then be trusting the thing under test to report on itself. What arrived at
// the driver is measured below the handle, so a statement the handle failed to
// record still shows up here.
func CountingHandle(t *testing.T) (*data.DB, *Statements) {
	t.Helper()

	counter := &Statements{}
	name := fmt.Sprintf("mcp-sections-%p", counter)
	sql.Register(name, &countingDriver{counter: counter})

	inner, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("opening the counting handle: %v", err)
	}
	t.Cleanup(func() { _ = inner.Close() })

	return data.Wrap(inner, data.DialectSQLite), counter
}

// Statements counts what reached the driver, and remembers the last one.
type Statements struct {
	count atomic.Int64

	mu   sync.Mutex
	last string
	args []driver.NamedValue
}

// Count is how many statements arrived.
func (s *Statements) Count() int { return int(s.count.Load()) }

// Last is the most recent statement and the values bound to it.
func (s *Statements) Last() (string, []driver.NamedValue) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last, s.args
}

func (s *Statements) record(query string, args []driver.NamedValue) {
	s.count.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last, s.args = query, args
}

// countingDriver answers one shape of result and counts every statement.
//
// It is the smallest thing database/sql accepts: the suite never asks it for
// anything but the two columns the service reads, and a driver that could do
// more would be a second place for a test to go wrong.
type countingDriver struct{ counter *Statements }

func (d *countingDriver) Open(string) (driver.Conn, error) {
	return &countingConn{counter: d.counter}, nil
}

type countingConn struct{ counter *Statements }

func (c *countingConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("this driver answers only the context methods")
}
func (c *countingConn) Close() error              { return nil }
func (c *countingConn) Begin() (driver.Tx, error) { return nil, errors.New("no transactions here") }

// QueryContext counts the statement and answers one row.
func (c *countingConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.counter.record(query, args)
	return &oneRow{}, nil
}

// ExecContext counts the statement and answers nothing.
func (c *countingConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.counter.record(query, args)
	return driver.RowsAffected(0), nil
}

// oneRow is a single section, which is enough for a caller to tell a result
// from no result.
type oneRow struct{ done bool }

func (r *oneRow) Columns() []string { return []string{"id", "name"} }
func (r *oneRow) Close() error      { return nil }
func (r *oneRow) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0], dest[1] = "s1", "Introduction"
	return nil
}

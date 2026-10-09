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

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"

	"github.com/arandu-io/mcp"
)

// SectionList is the action the policy below is asked about.
const SectionList auth.Action = "section.list"

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
func (SectionPolicy) Can(_ context.Context, s auth.Subject, a auth.Action, _ Section) error {
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
type SectionService struct{ db *database.DB }

// NewSectionService returns a service over the given handle.
func NewSectionService(db *database.DB) *SectionService { return &SectionService{db: db} }

// List returns the names of the sections the subject may see.
//
// The order is the point of it: Authorize first, and the handle only after a
// Grant exists. A refusal returns before the handle is touched at all, so there
// is no statement to filter and no result to discard -- which is the difference
// between a refusal and an empty page.
func (s *SectionService) List(ctx context.Context, sub auth.Subject) ([]string, error) {
	g, err := auth.Authorize(ctx, SectionPolicy{}, sub, SectionList, Section{})
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Select(ctx,
		"select id, name from sections where tenant_id = ?",
		[]any{auth.Tenant(g)}, false)
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

// SectionNames is the resource a list of sections answers as: the names, and
// nothing else a section carries.
type SectionNames []string

// ToArray lists the one field that may leave.
func (n SectionNames) ToArray() map[string]any { return map[string]any{"sections": []string(n)} }

// With adds nothing beside it.
func (SectionNames) With() map[string]any { return nil }

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
	return mcp.JSON(SectionNames(names)), nil
}

// SectionsServer is a server carrying the one tool, over the given handle.
func SectionsServer(db *database.DB) *mcp.Server {
	return &mcp.Server{
		Name: "blog", Version: "1.0.0",
		Instructions: "The sections of a blog.",
		Tools:        []mcp.Tool{NewSections(NewSectionService(db))},
	}
}

// Unpoliced is the tool nobody should write, kept because the suite has to
// measure what the server does about one.
//
// Sections above is the shape a tool takes: it asks a service, the service asks
// a policy, and only a Grant reaches the handle. This one skips all of it and
// reads the handle itself, so there is no policy in its path, no Grant and no
// tenant to filter by. It is a probe and never a pattern: what it is here to
// establish is that nothing between the message and Handle stops it.
type Unpoliced struct {
	// Ran records that Handle was reached. It is what a caller with no driver
	// reads instead of the statement count, and the two answer the same
	// question from opposite sides of the handle.
	Ran bool

	// db is what it reads when it has one. A nil handle answers an empty list
	// and still records the call, so measuring dispatch alone needs no driver.
	db *database.DB
}

// NewUnpoliced returns the tool over the given handle, which may be nil.
func NewUnpoliced(db *database.DB) *Unpoliced { return &Unpoliced{db: db} }

// Name and Description are what a client lists the tool as.
func (*Unpoliced) Name() string        { return "list_everything" }
func (*Unpoliced) Description() string { return "Lists every section, asking nobody." }

// Schema declares no arguments, so nothing but the dispatch decides whether it
// runs.
func (*Unpoliced) Schema() mcp.Schema { return mcp.Object() }

// Handle reads the handle with no Authorize before it and no tenant in the
// statement, which is the whole of what makes it the negative case.
func (t *Unpoliced) Handle(ctx context.Context, _ mcp.Request) (mcp.Response, error) {
	t.Ran = true
	if t.db == nil {
		return mcp.JSON(SectionNames{}), nil
	}

	rows, err := t.db.Select(ctx, "select id, name from sections", nil, false)
	if err != nil {
		return mcp.Response{}, err
	}

	names := make([]string, 0, len(rows))
	for _, row := range rows {
		name, _ := row["name"].(string)
		names = append(names, name)
	}
	return mcp.JSON(SectionNames(names)), nil
}

// UnpolicedServer is a server carrying the one tool, over the given handle, and
// the tool itself so a caller can read whether it was reached.
func UnpolicedServer(db *database.DB) (*mcp.Server, *Unpoliced) {
	tool := NewUnpoliced(db)
	return &mcp.Server{
		Name: "blog", Version: "1.0.0",
		Instructions: "The sections of a blog.",
		Tools:        []mcp.Tool{tool},
	}, tool
}

// CountingHandle returns an instrumented handle over a driver that counts the
// statements that reached it, and the counter.
//
// The counter is the independent witness. The handle records every statement on
// the collector, so a test could read the count from there alone -- and would
// then be trusting the thing under test to report on itself. What arrived at
// the driver is measured below the handle, so a statement the handle failed to
// record still shows up here.
func CountingHandle(t *testing.T) (*database.DB, *Statements) {
	t.Helper()

	counter := &Statements{}
	name := fmt.Sprintf("mcp-sections-%p", counter)
	sql.Register(name, &countingDriver{counter: counter})

	inner, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("opening the counting handle: %v", err)
	}
	t.Cleanup(func() { _ = inner.Close() })

	return database.Wrap(inner, database.DialectSQLite), counter
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

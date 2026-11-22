package catalog

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/shanghe000/ontology-workbench/internal/graph"
	_ "modernc.org/sqlite"
)

var (
	ErrMissing  = errors.New("space, revision or proposal not found")
	ErrConflict = errors.New("published revision changed, proposal already reviewed or request ID reused")
	ErrInvalid  = errors.New("invalid proposal")
)

type Space struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Head  int    `json:"head"`
}

type Submission struct {
	RequestID    string      `json:"request_id"`
	Title        string      `json:"title"`
	BaseRevision *int        `json:"base_revision"`
	Patch        graph.Patch `json:"patch"`
}

type Proposal struct {
	ID string `json:"id"`
	Submission
	Status            string `json:"status"`
	SubmittedAt       string `json:"submitted_at"`
	ReviewedAt        string `json:"reviewed_at,omitempty"`
	PublishedRevision *int   `json:"published_revision,omitempty"`
}

type Store struct {
	db *sql.DB
}

func Open(filename string) (*Store, error) {
	const options = "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"
	dsn := "file::memory:" + options
	if filename != ":memory:" {
		absolute, err := filepath.Abs(filename)
		if err != nil {
			return nil, err
		}
		dsn = (&url.URL{Scheme: "file", Path: absolute}).String() + options
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// A single connection also keeps the in-memory demonstration consistent.
	db.SetMaxOpenConns(1)
	const schema = `
CREATE TABLE IF NOT EXISTS spaces (
  id TEXT PRIMARY KEY, label TEXT NOT NULL, head INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS revisions (
  space TEXT NOT NULL REFERENCES spaces(id), number INTEGER NOT NULL,
  snapshot BLOB NOT NULL, PRIMARY KEY(space, number)
);
CREATE TABLE IF NOT EXISTS proposals (
  space TEXT NOT NULL REFERENCES spaces(id), id TEXT NOT NULL,
  request_id TEXT NOT NULL, input BLOB NOT NULL, status TEXT NOT NULL,
  submitted_at TEXT NOT NULL, reviewed_at TEXT NOT NULL DEFAULT '',
  published_revision INTEGER,
  PRIMARY KEY(space, id), UNIQUE(space, request_id)
);
`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) EnsureSpace(ctx context.Context, id, label string, seed graph.Snapshot) error {
	if !graph.ValidID(id) || label == "" || len(label) > 100 {
		return fmt.Errorf("invalid space identifier or label")
	}
	if err := graph.Validate(seed); err != nil {
		return err
	}
	raw, err := json.Marshal(seed)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, "INSERT INTO spaces(id,label,head) VALUES(?,?,?) ON CONFLICT DO NOTHING", id, label, seed.Revision)
	if err != nil {
		return err
	}
	if count, err := res.RowsAffected(); err != nil {
		return err
	} else if count == 1 {
		if _, err := tx.ExecContext(ctx, "INSERT INTO revisions VALUES(?,?,?)", id, seed.Revision, raw); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Spaces(ctx context.Context) ([]Space, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,label,head FROM spaces ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Space{}
	for rows.Next() {
		var space Space
		if err := rows.Scan(&space.ID, &space.Label, &space.Head); err != nil {
			return nil, err
		}
		out = append(out, space)
	}
	return out, rows.Err()
}

type rowSource interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func snapshot(ctx context.Context, db rowSource, space string, revision *int) (graph.Snapshot, error) {
	var row *sql.Row
	if revision == nil {
		row = db.QueryRowContext(ctx, "SELECT r.snapshot FROM revisions r JOIN spaces s ON s.id=r.space AND s.head=r.number WHERE r.space=?", space)
	} else {
		row = db.QueryRowContext(ctx, "SELECT snapshot FROM revisions WHERE space=? AND number=?", space, *revision)
	}
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return graph.Snapshot{}, missing(err)
	}
	var result graph.Snapshot
	err := json.Unmarshal(raw, &result)
	return result, err
}

func (s *Store) Graph(ctx context.Context, space string, revision *int) (graph.Snapshot, error) {
	return snapshot(ctx, s.db, space, revision)
}

const proposalFields = "id,input,status,submitted_at,reviewed_at,published_revision"

func readProposal(row interface{ Scan(...any) error }) (Proposal, []byte, error) {
	var p Proposal
	var raw []byte
	if err := row.Scan(&p.ID, &raw, &p.Status, &p.SubmittedAt, &p.ReviewedAt, &p.PublishedRevision); err != nil {
		return Proposal{}, nil, missing(err)
	}
	err := json.Unmarshal(raw, &p.Submission)
	return p, raw, err
}

func (s *Store) Submit(ctx context.Context, space string, input Submission) (Proposal, error) {
	input.Title = strings.TrimSpace(input.Title)
	if !graph.ValidID(input.RequestID) || input.Title == "" || len(input.Title) > 140 ||
		strings.ContainsFunc(input.Title, unicode.IsControl) || input.BaseRevision == nil || *input.BaseRevision < 0 {
		return Proposal{}, fmt.Errorf("%w: request ID, title and base revision are required", ErrInvalid)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return Proposal{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Proposal{}, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, "SELECT "+proposalFields+" FROM proposals WHERE space=? AND request_id=?", space, input.RequestID)
	if existing, original, err := readProposal(row); err == nil {
		if !bytes.Equal(raw, original) {
			return Proposal{}, ErrConflict
		}
		return existing, nil
	} else if !errors.Is(err, ErrMissing) {
		return Proposal{}, err
	}
	current, err := snapshot(ctx, tx, space, nil)
	if err != nil {
		return Proposal{}, err
	}
	if current.Revision != *input.BaseRevision {
		return Proposal{}, ErrConflict
	}
	if _, err := graph.Apply(current, input.Patch); err != nil {
		return Proposal{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	p := Proposal{ID: strings.ToLower(rand.Text()), Submission: input, Status: "pending", SubmittedAt: now()}
	if _, err := tx.ExecContext(ctx, "INSERT INTO proposals(space,id,request_id,input,status,submitted_at) VALUES(?,?,?,?,?,?)",
		space, p.ID, p.RequestID, raw, p.Status, p.SubmittedAt); err != nil {
		return Proposal{}, err
	}
	return p, tx.Commit()
}

func (s *Store) Proposals(ctx context.Context, space string) ([]Proposal, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+proposalFields+" FROM proposals WHERE space=? ORDER BY status != 'pending', submitted_at DESC, id LIMIT 30", space)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Proposal{}
	for rows.Next() {
		p, _, err := readProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Preview(ctx context.Context, space, id string) (Proposal, graph.Snapshot, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+proposalFields+" FROM proposals WHERE space=? AND id=?", space, id)
	p, _, err := readProposal(row)
	if err != nil {
		return Proposal{}, graph.Snapshot{}, err
	}
	base, err := s.Graph(ctx, space, p.BaseRevision)
	if err != nil {
		return Proposal{}, graph.Snapshot{}, err
	}
	candidate, err := graph.Apply(base, p.Patch)
	return p, candidate, err
}

// Review serializes version publication with its proposal status in one transaction.
func (s *Store) Review(ctx context.Context, space, id, decision string) (Proposal, error) {
	if decision != "approve" && decision != "reject" {
		return Proposal{}, fmt.Errorf("%w: expected approve or reject", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Proposal{}, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, "SELECT "+proposalFields+" FROM proposals WHERE space=? AND id=?", space, id)
	p, _, err := readProposal(row)
	if err != nil {
		return Proposal{}, err
	}
	if p.Status != "pending" {
		return Proposal{}, ErrConflict
	}
	p.Status, p.ReviewedAt = "rejected", now()
	if decision == "approve" {
		base, err := snapshot(ctx, tx, space, nil)
		if err != nil {
			return Proposal{}, err
		}
		if base.Revision != *p.BaseRevision {
			return Proposal{}, ErrConflict
		}
		candidate, err := graph.Apply(base, p.Patch)
		if err != nil {
			return Proposal{}, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		raw, err := json.Marshal(candidate)
		if err != nil {
			return Proposal{}, err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO revisions VALUES(?,?,?)", space, candidate.Revision, raw); err != nil {
			return Proposal{}, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE spaces SET head=? WHERE id=?", candidate.Revision, space); err != nil {
			return Proposal{}, err
		}
		p.Status, p.PublishedRevision = "approved", &candidate.Revision
	}
	_, err = tx.ExecContext(ctx, "UPDATE proposals SET status=?,reviewed_at=?,published_revision=? WHERE space=? AND id=?",
		p.Status, p.ReviewedAt, p.PublishedRevision, space, id)
	if err != nil {
		return Proposal{}, err
	}
	return p, tx.Commit()
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func missing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMissing
	}
	return err
}

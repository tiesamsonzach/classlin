package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Message struct {
	Seq       int64
	ID        string
	Channel   string
	Body      string
	ImageMime string
	CreatedMs int64
}

type ListResult struct {
	Messages   []Message
	NextBefore int64
}

type Store struct {
	db     *sql.DB
	imgDir string
}

func Open(dataDir string) (*Store, error) {
	imgDir := filepath.Join(dataDir, "images")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.ToSlash(filepath.Join(dataDir, "classroom.db"))
	dsn := "file:" + dbPath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)
	s := &Store{db: db, imgDir: imgDir}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS messages (
	seq INTEGER PRIMARY KEY AUTOINCREMENT,
	id TEXT NOT NULL UNIQUE,
	channel TEXT NOT NULL,
	body TEXT NOT NULL,
	image_mime TEXT NOT NULL DEFAULT '',
	created_ms INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_channel_seq ON messages(channel, seq DESC);
`)
	return err
}

func ValidID(id string) bool {
	if len(id) != 30 || id[13] != '-' {
		return false
	}
	for i := 0; i < 13; i++ {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	for i := 14; i < 30; i++ {
		c := id[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func newID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("%013d-%s", time.Now().UnixMilli(), hex.EncodeToString(buf[:])), nil
}

func (s *Store) Add(channel, body, mime string, image []byte) (Message, error) {
	return s.add(channel, body, mime, image, time.Now().UnixMilli())
}

func (s *Store) add(channel, body, mime string, image []byte, createdMs int64) (Message, error) {
	id, err := newID()
	if err != nil {
		return Message{}, err
	}
	if !ValidID(id) {
		return Message{}, errors.New("bad id")
	}
	if mime != "" {
		if err := s.writeImage(id, image); err != nil {
			return Message{}, err
		}
	}
	res, err := s.db.Exec(
		`INSERT INTO messages (id, channel, body, image_mime, created_ms) VALUES (?, ?, ?, ?, ?)`,
		id, channel, body, mime, createdMs,
	)
	if err != nil {
		if mime != "" {
			_ = os.Remove(filepath.Join(s.imgDir, id))
		}
		return Message{}, err
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return Message{}, err
	}
	return Message{
		Seq:       seq,
		ID:        id,
		Channel:   channel,
		Body:      body,
		ImageMime: mime,
		CreatedMs: createdMs,
	}, nil
}

func (s *Store) writeImage(id string, data []byte) error {
	final := filepath.Join(s.imgDir, id)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

func (s *Store) List(channel string, before int64, limit int) (ListResult, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.db.Query(`
SELECT seq, id, channel, body, image_mime, created_ms
FROM messages
WHERE channel = ? AND (? = 0 OR seq < ?)
ORDER BY seq DESC
LIMIT ?`, channel, before, before, limit)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()
	var out ListResult
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.Seq, &m.ID, &m.Channel, &m.Body, &m.ImageMime, &m.CreatedMs); err != nil {
			return ListResult{}, err
		}
		out.Messages = append(out.Messages, m)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	if out.Messages == nil {
		out.Messages = []Message{}
	}
	if len(out.Messages) == limit {
		out.NextBefore = out.Messages[len(out.Messages)-1].Seq
	}
	return out, nil
}

func (s *Store) Get(id string) (Message, error) {
	if !ValidID(id) {
		return Message{}, ErrNotFound
	}
	var m Message
	err := s.db.QueryRow(`
SELECT seq, id, channel, body, image_mime, created_ms
FROM messages WHERE id = ?`, id).Scan(&m.Seq, &m.ID, &m.Channel, &m.Body, &m.ImageMime, &m.CreatedMs)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrNotFound
	}
	if err != nil {
		return Message{}, err
	}
	return m, nil
}

func (s *Store) OpenImage(id string) (*os.File, error) {
	if !ValidID(id) {
		return nil, ErrNotFound
	}
	f, err := os.Open(filepath.Join(s.imgDir, id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

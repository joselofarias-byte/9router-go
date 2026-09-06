package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// FabricEnvelope carries the exact canonical UTF-8 payload bytes (base64 in JSON).
// Hash is integrity only. Administrative authentication authorizes writes.
type FabricEnvelope struct {
	Hash    string `json:"hash"`
	Payload []byte `json:"payload"`
}
type FabricNode struct {
	ID      string `json:"id"`
	BaseURL string `json:"baseUrl"`
}
type FabricCombo struct {
	Name   string   `json:"name"`
	Models []string `json:"models"`
}
type FabricSnapshot struct {
	SchemaVersion int           `json:"schemaVersion"`
	IssuedAt      string        `json:"issuedAt"`
	ExpiresAt     string        `json:"expiresAt"`
	Nodes         []FabricNode  `json:"nodes"`
	Combos        []FabricCombo `json:"combos"`
}
type FabricState struct {
	Current  *FabricEnvelope `json:"current"`
	Previous *FabricEnvelope `json:"previous"`
}

var ErrFabricConflict = errors.New("snapshot changed; fetch state and preview again")
var fabricID = regexp.MustCompile(`^fabric-[a-z0-9][a-z0-9-]{0,63}$`)

func ValidateFabric(e FabricEnvelope, now time.Time, rollback bool) (*FabricSnapshot, error) {
	if len(e.Payload) == 0 || len(e.Payload) > 1<<20 {
		return nil, errors.New("invalid payload size")
	}
	hash := sha256.Sum256(e.Payload)
	if hex.EncodeToString(hash[:]) != e.Hash {
		return nil, errors.New("snapshot hash mismatch")
	}
	var s FabricSnapshot
	dec := json.NewDecoder(strings.NewReader(string(e.Payload)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return nil, errors.New("invalid snapshot schema")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, errors.New("trailing snapshot data")
	}
	issued, err := time.Parse(time.RFC3339, s.IssuedAt)
	if err != nil {
		return nil, errors.New("invalid issuedAt")
	}
	expires, err := time.Parse(time.RFC3339, s.ExpiresAt)
	if err != nil || !expires.After(issued) || issued.After(now.Add(5*time.Minute)) || (!rollback && !expires.After(now)) {
		return nil, errors.New("invalid snapshot validity window")
	}
	if s.SchemaVersion != 1 || len(s.Nodes) > 256 || len(s.Combos) > 256 {
		return nil, errors.New("unsupported snapshot size or version")
	}
	ids := map[string]bool{}
	for _, n := range s.Nodes {
		u, err := url.Parse(n.BaseURL)
		if !fabricID.MatchString(n.ID) || ids[n.ID] || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("invalid or duplicate node")
		}
		ids[n.ID] = true
	}
	names := map[string]bool{}
	for _, c := range s.Combos {
		if !fabricID.MatchString(c.Name) || !(c.Name == "fabric-experimental" || strings.HasPrefix(c.Name, "fabric-experimental-")) || names[c.Name] || len(c.Models) == 0 || len(c.Models) > 256 {
			return nil, errors.New("only unique experimental combos are allowed")
		}
		names[c.Name] = true
		seen := map[string]bool{}
		for _, m := range c.Models {
			parts := strings.SplitN(m, "/", 2)
			if len(parts) != 2 || !ids[parts[0]] || parts[1] == "" || len(m) > 512 || strings.ContainsAny(m, "\r\n\x00") || seen[m] {
				return nil, errors.New("invalid combo model reference")
			}
			seen[m] = true
		}
	}
	return &s, nil
}

func (r *Repo) FabricState(ctx context.Context) (*FabricState, error) {
	// One SELECT gives a consistent read of both slots even during an apply.
	rows, err := r.db.QueryContext(ctx, `SELECT key,value FROM kv WHERE scope='fabric' AND key IN ('current','previous')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	state := &FabricState{}
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		var e FabricEnvelope
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			return nil, err
		}
		if key == "current" {
			state.Current = &e
		} else {
			state.Previous = &e
		}
	}
	return state, rows.Err()
}

func (r *Repo) ApplyFabric(ctx context.Context, incoming *FabricEnvelope, expected string, rollback bool) (*FabricEnvelope, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Acquire the SQLite write lock before reading. No lost update across processes.
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO kv(scope,key,value) VALUES('fabric','lock','1')`); err != nil {
		return nil, err
	}
	read := func(key string) (*FabricEnvelope, error) {
		var raw string
		err := tx.QueryRowContext(ctx, `SELECT value FROM kv WHERE scope='fabric' AND key=?`, key).Scan(&raw)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		var e FabricEnvelope
		err = json.Unmarshal([]byte(raw), &e)
		return &e, err
	}
	current, err := read("current")
	if err != nil {
		return nil, err
	}
	currentHash := ""
	if current != nil {
		currentHash = current.Hash
	}
	if currentHash != expected {
		return nil, ErrFabricConflict
	}
	if rollback {
		incoming, err = read("previous")
		if err != nil {
			return nil, err
		}
	}
	if incoming == nil {
		return nil, errors.New("no snapshot to apply")
	}
	next, err := ValidateFabric(*incoming, time.Now(), rollback)
	if err != nil {
		return nil, err
	}
	old := &FabricSnapshot{}
	if current != nil {
		old, err = ValidateFabric(*current, time.Now(), true)
		if err != nil {
			return nil, err
		}
	}

	// Refuse to overwrite routing edited by management after the last apply.
	for _, n := range old.Nodes {
		var raw string
		if err := tx.QueryRowContext(ctx, `SELECT data FROM providerNodes WHERE id=?`, n.ID).Scan(&raw); err != nil {
			return nil, ErrFabricConflict
		}
		var data ProviderNodeData
		if json.Unmarshal([]byte(raw), &data) != nil || data.BaseURL != n.BaseURL || data.Prefix != n.ID || data.APIType != "chat-completions" {
			return nil, ErrFabricConflict
		}
	}
	for _, c := range old.Combos {
		var raw string
		if err := tx.QueryRowContext(ctx, `SELECT models FROM combos WHERE id=? AND name=?`, c.Name, c.Name).Scan(&raw); err != nil {
			return nil, ErrFabricConflict
		}
		var ms []string
		if json.Unmarshal([]byte(raw), &ms) != nil || len(ms) != len(c.Models) {
			return nil, ErrFabricConflict
		}
		for i := range ms {
			if ms[i] != c.Models[i] {
				return nil, ErrFabricConflict
			}
		}
	}
	if incoming.Hash == currentHash {
		return current, nil
	}
	oldNodes := map[string]bool{}
	oldCombos := map[string]bool{}
	for _, n := range old.Nodes {
		oldNodes[n.ID] = true
	}
	for _, c := range old.Combos {
		oldCombos[c.Name] = true
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, n := range next.Nodes {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM providerNodes WHERE id=? OR json_extract(data,'$.prefix')=?`, n.ID, n.ID).Scan(&count); err != nil {
			return nil, err
		}
		if !oldNodes[n.ID] && count != 0 {
			return nil, errors.New("node ownership conflict")
		}
		raw, _ := json.Marshal(map[string]string{"prefix": n.ID, "apiType": "chat-completions", "baseUrl": n.BaseURL})
		_, err = tx.ExecContext(ctx, `INSERT INTO providerNodes(id,type,name,data,createdAt,updatedAt) VALUES(?,'openai-compatible',?,?,?,?) ON CONFLICT(id) DO UPDATE SET data=json_set(providerNodes.data,'$.prefix',json_extract(excluded.data,'$.prefix'),'$.baseUrl',json_extract(excluded.data,'$.baseUrl'),'$.apiType',json_extract(excluded.data,'$.apiType')), updatedAt=excluded.updatedAt`, n.ID, n.ID, string(raw), now, now)
		if err != nil {
			return nil, err
		}
		delete(oldNodes, n.ID)
	}
	for _, c := range next.Combos {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM combos WHERE id=? OR name=?`, c.Name, c.Name).Scan(&count); err != nil {
			return nil, err
		}
		if !oldCombos[c.Name] && count != 0 {
			return nil, errors.New("combo ownership conflict")
		}
		raw, _ := json.Marshal(c.Models)
		_, err = tx.ExecContext(ctx, `INSERT INTO combos(id,name,kind,models,createdAt,updatedAt) VALUES(?,?,'chat',?,?,?) ON CONFLICT(id) DO UPDATE SET models=excluded.models,updatedAt=excluded.updatedAt`, c.Name, c.Name, string(raw), now, now)
		if err != nil {
			return nil, err
		}
		delete(oldCombos, c.Name)
	}
	for name := range oldCombos {
		if _, err = tx.ExecContext(ctx, `DELETE FROM combos WHERE id=? AND name=?`, name, name); err != nil {
			return nil, err
		}
	}
	for id := range oldNodes {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM providerConnections WHERE provider=?`, id).Scan(&count); err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, fmt.Errorf("node %s still has accounts; detach them before removing node", id)
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM providerNodes WHERE id=?`, id); err != nil {
			return nil, err
		}
	}
	write := func(key string, e *FabricEnvelope) error {
		if e == nil {
			return nil
		}
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO kv(scope,key,value) VALUES('fabric',?,?) ON CONFLICT(scope,key) DO UPDATE SET value=excluded.value`, key, string(raw))
		return err
	}
	if err = write("previous", current); err != nil {
		return nil, err
	}
	if err = write("current", incoming); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return incoming, nil
}

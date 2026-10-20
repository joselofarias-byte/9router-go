package db

import (
    "bytes"
    "crypto/rand"
    "os"
    "path/filepath"
    "testing"
)

func TestGROK004DB02SyntheticSQLiteEncryptedLookup(t *testing.T) {
    // This deliberately uses a new synthetic database and table. No existing
    // credentials, schema, migrations or production rows are touched.
    path := filepath.Join(t.TempDir(), "synthetic-keys.sqlite")
    connection, err := OpenDatabase(path)
    if err != nil { t.Fatal(err) }
    defer connection.Close()
    _, err = connection.Exec(`CREATE TABLE grok004PrototypeKeys(
        id TEXT PRIMARY KEY, keyLookupHMAC TEXT NOT NULL UNIQUE,
        encryptedKey TEXT NOT NULL
    )`)
    if err != nil { t.Fatal(err) }

    master := make([]byte, 32)
    if _, err := rand.Read(master); err != nil { t.Fatal(err) }
    bearer := "grok004-synthetic-private-bearer-canary"
    lookup, err := apiKeyLookupHMAC(master, bearer)
    if err != nil { t.Fatal(err) }
    encrypted, err := encryptDBField(master, "grok004PrototypeKeys", "key-1", "encryptedKey", []byte(bearer))
    if err != nil { t.Fatal(err) }
    _, err = connection.Exec(
        `INSERT INTO grok004PrototypeKeys(id, keyLookupHMAC, encryptedKey) VALUES(?, ?, ?)`,
        "key-1", lookup, encrypted)
    if err != nil { t.Fatal(err) }

    var value string
    if err := connection.QueryRow(`SELECT encryptedKey FROM grok004PrototypeKeys WHERE keyLookupHMAC = ?`, lookup).Scan(&value); err != nil {
        t.Fatal(err)
    }
    recovered, err := decryptDBField(master, "grok004PrototypeKeys", "key-1", "encryptedKey", value)
    if err != nil || string(recovered) != bearer {
        t.Fatalf("authenticated lookup failed: %v", err)
    }
    badIndex, err := apiKeyLookupHMAC(master, bearer+"-different")
    if err != nil { t.Fatal(err) }
    var exists int
    err = connection.QueryRow(`SELECT COUNT(*) FROM grok004PrototypeKeys WHERE keyLookupHMAC = ?`, badIndex).Scan(&exists)
    if err != nil || exists != 0 {
        t.Fatalf("unexpected lookup match: count=%d err=%v", exists, err)
    }

    // Neither DB file nor its WAL should contain the synthetic token in a
    // freshly encrypted-only deployment (not a guarantee for old migrated DBs).
    for _, filename := range []string{path, path+"-wal"} {
        raw, err := os.ReadFile(filename)
        if os.IsNotExist(err) { continue }
        if err != nil { t.Fatal(err) }
        if bytes.Contains(raw, []byte(bearer)) {
            t.Fatalf("plaintext leaked into synthetic SQLite file %s", filepath.Base(filename))
        }
    }
}

// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/migrations"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm/schema"
)

// Every column a live model declares must exist in the numbered migrations.
//
// This exists because of a production outage. Three progress columns were added
// to the QueuedTask model on the assumption that AutoMigrate would create them.
// It does not: AutoMigrate runs only when KIWI_AUTOMIGRATE=true
// (pkg/orchestrator/db.go), which production does not set, and the `migrate`
// role runs RunMigrations alone. So the columns existed in Go and nowhere in
// the database, and every enqueue failed on an INSERT naming a column that was
// not there — taking out task submission entirely, not merely the feature.
//
// The existing migration test cannot catch this: it skips unless
// KIWI_TEST_PG_DSN is set, so it never runs in CI. This one needs no database.
// It reads the model with reflection and greps the migration SQL, which is
// crude but catches exactly the mistake that caused the outage: a field added
// to a Go struct with no migration behind it.
func TestQueuedTaskColumnsExistInMigrations(t *testing.T) {
	sql := allMigrationSQL(t)

	// GORM's default naming strategy is what turns a field name into a column
	// name, so the test must use the same one rather than approximate it.
	ns := schema.NamingStrategy{}

	rt := reflect.TypeOf(store.QueuedTask{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("gorm")
		if strings.Contains(tag, "-") && strings.HasPrefix(strings.TrimSpace(tag), "-") {
			continue // explicitly not a column
		}

		col := columnFromTag(tag)
		if col == "" {
			col = ns.ColumnName("", f.Name)
		}
		if !strings.Contains(sql, col) {
			t.Errorf("QueuedTask.%s maps to column %q, which appears in no migration.\n"+
				"Production does not run AutoMigrate (KIWI_AUTOMIGRATE is unset), so a column that "+
				"exists only in the Go model does not exist in the database, and every insert fails.\n"+
				"Add a numbered migration in migrations/.", f.Name, col)
		}
	}
}

// The same guarantee for the other table that exists only via AutoMigrate and
// is written on the hot path.
func TestCredentialColumnsExistInMigrations(t *testing.T) {
	sql := allMigrationSQL(t)
	ns := schema.NamingStrategy{}

	rt := reflect.TypeOf(store.Credential{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		col := columnFromTag(f.Tag.Get("gorm"))
		if col == "" {
			col = ns.ColumnName("", f.Name)
		}
		if !strings.Contains(sql, col) {
			t.Errorf("Credential.%s maps to column %q, which appears in no migration", f.Name, col)
		}
	}
}

// columnFromTag returns an explicit `column:` name from a gorm tag, or "".
func columnFromTag(tag string) string {
	for _, part := range strings.Split(tag, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "column:") {
			return strings.TrimPrefix(part, "column:")
		}
	}
	return ""
}

func allMigrationSQL(t *testing.T) string {
	t.Helper()
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var b strings.Builder
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		data, err := migrations.FS.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		t.Fatal("no migrations found; the guard would pass vacuously")
	}
	return b.String()
}

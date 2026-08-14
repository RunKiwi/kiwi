// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/migrations"
	"gorm.io/gorm/schema"
)

// Every column a live model declares must exist in the numbered migrations.
//
// Production runs numbered SQL migrations only (KIWI_AUTOMIGRATE is unset),
// never GORM AutoMigrate — see ee/orchestrator/schema_drift_test.go for the
// outage that made this the rule. This is the ee/auth-side guard for the
// same mistake: a field added to User or DashboardSession with no migration
// behind it would exist in Go and nowhere in the database.
func TestUserColumnsExistInMigrations(t *testing.T) {
	assertColumnsInMigrations(t, User{})
}

func TestDashboardSessionColumnsExistInMigrations(t *testing.T) {
	assertColumnsInMigrations(t, DashboardSession{})
}

// Known limitation: this is a substring match against all .up.sql files
// concatenated together, not scoped to the specific table a column belongs
// to — a column name that happens to also appear in some other table's
// migration (e.g. a generic "id" or "org_id") would satisfy this check even
// if the column being tested was never actually added to ITS table. This
// matches the same simplification in the pre-existing
// ee/orchestrator/schema_drift_test.go this pattern was ported from. Fine
// for the specific, sufficiently-distinctive columns this test currently
// checks; be more careful if you ever add a check for a short/generic name.
func assertColumnsInMigrations(t *testing.T, model interface{}) {
	t.Helper()
	sql := allMigrationSQL(t)
	ns := schema.NamingStrategy{}

	rt := reflect.TypeOf(model)
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("gorm")
		if strings.Contains(tag, "-") && strings.HasPrefix(strings.TrimSpace(tag), "-") {
			continue // explicitly not a column
		}

		col := columnFromGormTag(tag)
		if col == "" {
			col = ns.ColumnName("", f.Name)
		}
		if !strings.Contains(sql, col) {
			t.Errorf("%s.%s maps to column %q, which appears in no migration.\n"+
				"Production does not run AutoMigrate (KIWI_AUTOMIGRATE is unset), so a column that "+
				"exists only in the Go model does not exist in the database, and every insert fails.\n"+
				"Add a numbered migration in migrations/.", rt.Name(), f.Name, col)
		}
	}
}

func columnFromGormTag(tag string) string {
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
		if !strings.HasSuffix(e.Name(), ".up.sql") {
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

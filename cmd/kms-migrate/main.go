package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/crypto"
	"github.com/ibreakthecloud/kiwi/pkg/orchestrator"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func main() {
	dsn := flag.String("dsn", "", "The Postgres DSN")
	flag.Parse()

	if *dsn == "" {
		*dsn = os.Getenv("KIWI_DSN")
		if *dsn == "" {
			log.Fatal("DSN is required (via -dsn or KIWI_DSN)")
		}
	}

	kmsKey := os.Getenv("KIWI_KMS_KEY")
	if kmsKey == "" {
		log.Fatal("KIWI_KMS_KEY must be set to re-wrap credentials with KMS")
	}

	ctx := context.Background()

	// Initialize both managers directly
	envMgr, err := crypto.NewEnvKeyManager()
	if err != nil {
		log.Fatalf("failed to init EnvKeyManager: %v", err)
	}

	kmsMgr, err := crypto.NewKMSKeyManager(ctx, kmsKey)
	if err != nil {
		log.Fatalf("failed to init KMSKeyManager: %v", err)
	}

	db, err := orchestrator.InitDB(*dsn)
	if err != nil {
		log.Fatalf("failed to init DB: %v", err)
	}

	log.Println("Starting KMS credential re-wrap migration...")

	var creds []store.Credential
	if err := db.Find(&creds).Error; err != nil {
		log.Fatalf("failed to fetch credentials: %v", err)
	}

	migrated := 0
	skipped := 0
	failed := 0

	for _, cred := range creds {
		if strings.HasPrefix(cred.EncryptedValue, "kms:v1:") {
			skipped++
			continue
		}

		// Decrypt legacy
		pt, err := envMgr.Decrypt(ctx, cred.EncryptedValue)
		if err != nil {
			log.Printf("failed to decrypt cred %s (org %s) with legacy key: %v", cred.ID, cred.OrgID, err)
			failed++
			continue
		}

		// Re-encrypt with KMS
		ct, err := kmsMgr.Encrypt(ctx, pt)
		if err != nil {
			log.Printf("failed to encrypt cred %s with KMS: %v", cred.ID, err)
			failed++
			continue
		}

		// Save back
		if err := db.Model(&store.Credential{}).Where("id = ?", cred.ID).Update("encrypted_value", ct).Error; err != nil {
			log.Printf("failed to update cred %s in DB: %v", cred.ID, err)
			failed++
			continue
		}

		migrated++
	}

	log.Printf("Migration complete. Migrated: %d, Skipped: %d, Failed: %d", migrated, skipped, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

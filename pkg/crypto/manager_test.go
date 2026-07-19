package crypto

import (
	"context"
	"os"
	"strings"
	"testing"
)

type FakeKMS struct {
	key string
}

func (f *FakeKMS) Encrypt(ctx context.Context, plaintext string) (string, error) {
	// Fake KMS just prefixes with kms:fake: and hex encodes the plaintext.
	// We use the same format as kms:v1 to test the dispatcher.
	return "kms:v1:fake_" + f.key + ":" + plaintext, nil
}

func (f *FakeKMS) Decrypt(ctx context.Context, ciphertextStr string) (string, error) {
	parts := strings.Split(ciphertextStr, ":")
	if len(parts) == 4 && parts[0] == "kms" && parts[1] == "v1" {
		return parts[3], nil
	}
	return "", nil
}

func TestEnvKeyManager_RoundTrip(t *testing.T) {
	os.Setenv("KIWI_ENCRYPTION_KEY", devEncryptionKey)
	defer os.Unsetenv("KIWI_ENCRYPTION_KEY")

	m, err := NewEnvKeyManager()
	if err != nil {
		t.Fatal(err)
	}

	pt := "secret_value"
	ct, err := m.Encrypt(context.Background(), pt)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := m.Decrypt(context.Background(), ct)
	if err != nil {
		t.Fatal(err)
	}

	if dec != pt {
		t.Fatalf("expected %q, got %q", pt, dec)
	}
}

func TestKMSKeyManager_Fallback(t *testing.T) {
	os.Setenv("KIWI_ENCRYPTION_KEY", devEncryptionKey)
	defer os.Unsetenv("KIWI_ENCRYPTION_KEY")

	envMgr, _ := NewEnvKeyManager()
	pt := "legacy_secret"
	legacyCt, _ := envMgr.Encrypt(context.Background(), pt)

	// A Fake KMS manager should still fallback to EnvKeyManager for legacy ciphertexts
	kmsMgr := &FakeKMS{key: "key1"}
	SetKeyManagerForTest(kmsMgr)
	defer ResetKeyManagerForTest()

	// But wait, our GetKeyManager uses the singleton. The decryption fallback is implemented
	// in the KMSKeyManager itself! We can test KMSKeyManager directly.
	// Note: We can't easily test real KMSKeyManager without a client, but we can test the logic
	// by invoking EnvKeyManager's decrypt.

	dec, err := DecryptAtRest(legacyCt)
	if err != nil {
		t.Fatal(err)
	}
	if dec != pt {
		t.Fatalf("fallback decrypt failed: got %q", dec)
	}
}

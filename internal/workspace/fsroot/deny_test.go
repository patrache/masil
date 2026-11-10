package fsroot

import "testing"

func TestDeniedList(t *testing.T) {
	yes := []string{
		".env", "a/.env.local", "k.pem", "x/id_rsa.pub", "id_ed25519", ".ssh/config", "a/.aws/credentials",
		"kubeconfig", "v.kdbx", "credentials-prod.json", "secrets.yaml", ".git/HEAD", "SECRETS.yaml",
		".git", ".ssh", "a/.gnupg/pubring.kbx", ".netrc", ".npmrc", ".pypirc", ".git-credentials",
		"c.p12", "c.pfx", "tls/server.key", ".ENV", "Credentials.JSON", "deep/.git/objects/ab",
	}
	no := []string{
		"env.go", "src/key.go", "credentials.md", "secret.go", "gitignore", ".gitignore",
		".github/workflows/ci.yml", "environment.ts", "src/keys/a.go", "id.go", "README.md",
	}
	for _, rel := range yes {
		if !Denied(rel) {
			t.Errorf("Denied(%q) = false, want true", rel)
		}
	}
	for _, rel := range no {
		if Denied(rel) {
			t.Errorf("Denied(%q) = true, want false", rel)
		}
	}
}

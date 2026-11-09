package apitool

import (
	"context"

	"github.com/patrache/masil/internal/secrets"
)

// ToolID is the tool id of the API test tool (secret keys, tool registry).
const ToolID = "api"

// Vault is where secret variable values live (the secrets store, C3.10 layer
// 1). Keys are SecretKey names. Get returns secrets.ErrNotFound for a missing
// value; Put returns secrets.ErrNeedConsent unchanged when the OS store is
// unavailable and no fallback was chosen (the screen asks, then retries).
type Vault interface {
	Get(key string) (secrets.Secret, error)
	Put(key string, v secrets.Secret) error
	Delete(key string) error
	Where(key string) secrets.Backend
}

// NewVault is the Vault over st for one masil account: the slot of key is
// secrets.Key{Tool: "api", Server: serverHost, UserID: userID, Name: key}.
// ctx is passed to every store call.
func NewVault(ctx context.Context, st *secrets.Store, serverHost, userID string) Vault {
	return &storeVault{ctx: ctx, st: st, server: serverHost, user: userID}
}

type storeVault struct {
	ctx          context.Context
	st           *secrets.Store
	server, user string
}

func (v *storeVault) key(name string) secrets.Key {
	return secrets.Key{Tool: ToolID, Server: v.server, UserID: v.user, Name: name}
}

func (v *storeVault) Get(key string) (secrets.Secret, error) {
	s, _, err := v.st.Get(v.ctx, v.key(key))
	return s, err
}

func (v *storeVault) Put(key string, s secrets.Secret) error {
	_, err := v.st.Put(v.ctx, v.key(key), s)
	return err
}

func (v *storeVault) Delete(key string) error { return v.st.Delete(v.ctx, v.key(key)) }

func (v *storeVault) Where(key string) secrets.Backend { return v.st.Where(v.key(key)) }

// SecretKey names the vault slot of a secret variable: a personal environment
// "local/<envID>/<name>", a shared collection's environment
// "doc/<docID>/<envID>/<name>", my globals "globals/<name>" (envID "").
func SecretKey(docID, envID, name string) string {
	switch {
	case envID == "":
		return "globals/" + name
	case docID == "":
		return "local/" + envID + "/" + name
	default:
		return "doc/" + docID + "/" + envID + "/" + name
	}
}

package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"
)

// defaultOS is the Secret Service on the session bus (godbus, no secret-tool).
func defaultOS() OSBackend {
	return secretService{env: os.Getenv, dial: dialSecretService}
}

const (
	ssName       = "org.freedesktop.secrets"
	ssPath       = dbus.ObjectPath("/org/freedesktop/secrets")
	ssService    = "org.freedesktop.Secret.Service"
	ssCollection = "org.freedesktop.Secret.Collection"
	ssItem       = "org.freedesktop.Secret.Item"
	ssPrompt     = "org.freedesktop.Secret.Prompt"
	ssSession    = "org.freedesktop.Secret.Session"
)

// ssSecret is the Secret Service Secret struct (oayays).
type ssSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

// dialSecretService opens a private connection to address (never autolaunched).
func dialSecretService(_ context.Context, address string) (dbusConn, error) {
	conn, err := dbus.Connect(address)
	if err != nil {
		return nil, err
	}
	return &godbusSS{conn: conn}, nil
}

// godbusSS is the dbusConn over godbus.
type godbusSS struct {
	conn    *dbus.Conn
	session dbus.ObjectPath
}

func (g *godbusSS) obj(p dbus.ObjectPath) dbus.BusObject { return g.conn.Object(ssName, p) }

func paths(ps []dbus.ObjectPath) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = string(p)
	}
	return out
}

func (g *godbusSS) OpenSession(ctx context.Context, algorithm string) (string, error) {
	var out dbus.Variant
	var session dbus.ObjectPath
	if err := g.obj(ssPath).CallWithContext(ctx, ssService+".OpenSession", 0, algorithm, dbus.MakeVariant("")).Store(&out, &session); err != nil {
		return "", err
	}
	g.session = session
	return string(session), nil
}

func (g *godbusSS) SearchItems(ctx context.Context, attrs map[string]string) ([]string, []string, error) {
	var unlocked, locked []dbus.ObjectPath
	if err := g.obj(ssPath).CallWithContext(ctx, ssService+".SearchItems", 0, attrs).Store(&unlocked, &locked); err != nil {
		return nil, nil, err
	}
	return paths(unlocked), paths(locked), nil
}

func (g *godbusSS) Unlock(ctx context.Context, objects []string) ([]string, string, error) {
	in := make([]dbus.ObjectPath, len(objects))
	for i, o := range objects {
		in[i] = dbus.ObjectPath(o)
	}
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := g.obj(ssPath).CallWithContext(ctx, ssService+".Unlock", 0, in).Store(&unlocked, &prompt); err != nil {
		return nil, "", err
	}
	return paths(unlocked), string(prompt), nil
}

func (g *godbusSS) Prompt(ctx context.Context, prompt string) (bool, error) {
	p := dbus.ObjectPath(prompt)
	match := []dbus.MatchOption{dbus.WithMatchObjectPath(p), dbus.WithMatchInterface(ssPrompt), dbus.WithMatchMember("Completed")}
	if err := g.conn.AddMatchSignalContext(ctx, match...); err != nil {
		return false, err
	}
	defer func() { _ = g.conn.RemoveMatchSignal(match...) }()
	ch := make(chan *dbus.Signal, 4)
	g.conn.Signal(ch)
	defer g.conn.RemoveSignal(ch)
	if err := g.obj(p).CallWithContext(ctx, ssPrompt+".Prompt", 0, "").Err; err != nil {
		return false, err
	}
	for {
		select {
		case sig, ok := <-ch:
			if !ok {
				return false, errors.New("connection closed")
			}
			if sig.Path != p || sig.Name != ssPrompt+".Completed" {
				continue
			}
			if len(sig.Body) == 0 {
				return false, errors.New("prompt: empty Completed signal")
			}
			dismissed, _ := sig.Body[0].(bool)
			return dismissed, nil
		case <-ctx.Done():
			_ = g.obj(p).Call(ssPrompt+".Dismiss", 0).Err
			return false, ctx.Err()
		}
	}
}

func (g *godbusSS) DefaultCollection(ctx context.Context) (string, error) {
	var coll dbus.ObjectPath
	if err := g.obj(ssPath).CallWithContext(ctx, ssService+".ReadAlias", 0, "default").Store(&coll); err != nil {
		return "", err
	}
	return string(coll), nil
}

func (g *godbusSS) Locked(ctx context.Context, collection string) (bool, error) {
	var v dbus.Variant
	if err := g.obj(dbus.ObjectPath(collection)).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, ssCollection, "Locked").Store(&v); err != nil {
		return false, err
	}
	locked, ok := v.Value().(bool)
	if !ok {
		return false, fmt.Errorf("collection Locked is %s", v.Signature())
	}
	return locked, nil
}

func (g *godbusSS) CreateItem(ctx context.Context, collection, label string, attrs map[string]string, session string, secret []byte, replace bool) (string, string, error) {
	props := map[string]dbus.Variant{
		ssItem + ".Label":      dbus.MakeVariant(label),
		ssItem + ".Attributes": dbus.MakeVariant(attrs),
	}
	s := ssSecret{Session: dbus.ObjectPath(session), Parameters: []byte{}, Value: secret, ContentType: "text/plain; charset=utf8"}
	var item, prompt dbus.ObjectPath
	if err := g.obj(dbus.ObjectPath(collection)).CallWithContext(ctx, ssCollection+".CreateItem", 0, props, s, replace).Store(&item, &prompt); err != nil {
		return "", "", err
	}
	return string(item), string(prompt), nil
}

func (g *godbusSS) SetSecret(ctx context.Context, item, session string, secret []byte) error {
	s := ssSecret{Session: dbus.ObjectPath(session), Parameters: []byte{}, Value: secret, ContentType: "text/plain; charset=utf8"}
	return g.obj(dbus.ObjectPath(item)).CallWithContext(ctx, ssItem+".SetSecret", 0, s).Err
}

func (g *godbusSS) GetSecret(ctx context.Context, item, session string) ([]byte, error) {
	var s ssSecret
	if err := g.obj(dbus.ObjectPath(item)).CallWithContext(ctx, ssItem+".GetSecret", 0, dbus.ObjectPath(session)).Store(&s); err != nil {
		return nil, err
	}
	return s.Value, nil
}

func (g *godbusSS) DeleteItem(ctx context.Context, item string) (string, error) {
	var prompt dbus.ObjectPath
	if err := g.obj(dbus.ObjectPath(item)).CallWithContext(ctx, ssItem+".Delete", 0).Store(&prompt); err != nil {
		return "", err
	}
	return string(prompt), nil
}

func (g *godbusSS) Close() error {
	if g.session != "" && g.session != "/" {
		_ = g.obj(g.session).Call(ssSession+".Close", 0).Err
	}
	return g.conn.Close()
}

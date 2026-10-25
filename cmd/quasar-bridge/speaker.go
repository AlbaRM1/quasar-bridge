package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
)

type speakerBackup struct {
	Config string         `json:"config"`
	Local  string         `json:"local"`
	URL    jsontext.Value `json:"url"`
	QUIC   jsontext.Value `json:"quic,omitempty"`
}

// Patch only the two endpoints. All firmware-specific members remain intact.
func setSpeakerEndpoint(path, backupPath, local, action string) error {
	if action != "apply" && action != "rollback" {
		return errors.New("speaker-endpoint must be apply or rollback")
	}
	u, err := url.Parse(local)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/uni.ws" || (u.Scheme != "ws" && u.Scheme != "wss") {
		return errors.New("speaker URL must be a loopback WebSocket /uni.ws endpoint")
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil || !ip.IsLoopback() {
		return errors.New("speaker URL must use a loopback IP")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return errors.New("cannot read speaker config")
	}
	var doc map[string]jsontext.Value
	if json.Unmarshal(raw, &doc) != nil {
		return errors.New("invalid speaker JSON")
	}
	var alice map[string]jsontext.Value
	if json.Unmarshal(doc["aliced"], &alice) != nil || alice == nil {
		return errors.New("missing aliced configuration")
	}
	var current string
	_ = json.Unmarshal(alice["uniProxyUrl"], &current)
	var saved speakerBackup
	previous, readErr := os.ReadFile(backupPath)
	if readErr == nil {
		if json.Unmarshal(previous, &saved) != nil || saved.Config != path || saved.Local != local {
			return errors.New("endpoint backup does not match this configuration")
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return errors.New("cannot read endpoint backup")
	}
	if action == "apply" {
		if readErr != nil {
			saved = speakerBackup{Config: path, Local: local, URL: alice["uniProxyUrl"], QUIC: alice["uniProxyQuicUrl"]}
			if saved.URL == nil {
				return errors.New("missing original endpoint")
			}
			data, _ := json.Marshal(saved)
			if err := os.MkdirAll(filepath.Dir(backupPath), 0700); err != nil {
				return errors.New("cannot create endpoint backup directory")
			}
			f, err := os.OpenFile(backupPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return errors.New("cannot create endpoint backup")
			}
			_, err = f.Write(data)
			if err == nil {
				err = f.Sync()
			}
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				return errors.New("cannot save endpoint backup")
			}
		} else {
			expectedURL, expectedQUIC := saved.URL, saved.QUIC
			if current == local {
				expectedURL, _ = json.Marshal(local)
				expectedQUIC = jsontext.Value(`""`)
			}
			if !sameEndpoint(alice["uniProxyUrl"], expectedURL) || !sameEndpoint(alice["uniProxyQuicUrl"], expectedQUIC) {
				return errors.New("speaker endpoints changed externally; inspect the saved backup")
			}
		}
		alice["uniProxyUrl"], _ = json.Marshal(local)
		alice["uniProxyQuicUrl"] = jsontext.Value(`""`)
	} else {
		if readErr != nil {
			return errors.New("no endpoint backup")
		}
		if current != local || !sameEndpoint(alice["uniProxyQuicUrl"], jsontext.Value(`""`)) {
			return errors.New("speaker endpoints changed externally; refusing rollback")
		}
		alice["uniProxyUrl"] = saved.URL
		if saved.QUIC != nil {
			alice["uniProxyQuicUrl"] = saved.QUIC
		} else {
			delete(alice, "uniProxyQuicUrl")
		}
	}
	doc["aliced"], _ = json.Marshal(alice)
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".quasar-bridge-*")
	if err != nil {
		return errors.New("cannot stage speaker config; remount the filesystem writable")
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(info.Mode().Perm()); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot save speaker config")
	}
	if err := os.Rename(tmp, path); err != nil {
		return errors.New("cannot replace speaker config")
	}
	if action == "rollback" {
		return os.Remove(backupPath)
	}
	return nil
}

func sameEndpoint(a, b jsontext.Value) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

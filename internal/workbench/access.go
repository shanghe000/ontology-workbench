package workbench

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/shanghe000/ontology-workbench/internal/graph"
)

type tokenHash [sha256.Size]byte

type spaceAccess struct {
	read, propose tokenHash
}

type Access struct {
	review tokenHash
	spaces map[string]spaceAccess
}

func LoadAccess(filename string, demo bool) (Access, error) {
	if filename == "" {
		if demo {
			return Access{}, nil
		}
		return Access{}, fmt.Errorf("set WORKBENCH_ACCESS_FILE for a persistent workbench")
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return Access{}, fmt.Errorf("cannot read the access file")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 65536 {
		return Access{}, fmt.Errorf("access file must be a private regular file (mode 0600; max 64 KiB)")
	}
	f, err := os.Open(filename)
	if err != nil {
		return Access{}, fmt.Errorf("cannot open the access file")
	}
	defer f.Close()
	var config struct {
		Review string `json:"review_token"`
		Spaces map[string]struct {
			Read    string `json:"read"`
			Propose string `json:"propose"`
		} `json:"spaces"`
	}
	dec := json.NewDecoder(io.LimitReader(f, 65537))
	dec.DisallowUnknownFields()
	if dec.Decode(&config) != nil || dec.Decode(new(any)) != io.EOF {
		return Access{}, fmt.Errorf("invalid access file JSON")
	}
	access := Access{spaces: make(map[string]spaceAccess)}
	seen := make(map[tokenHash]bool)
	add := func(value string) (tokenHash, error) {
		sum := sha256.Sum256([]byte(value))
		if len(value) < 32 || len(value) > 256 ||
			strings.ContainsFunc(value, func(r rune) bool { return r <= ' ' || r >= 127 }) || seen[sum] {
			return tokenHash{}, fmt.Errorf("access tokens must be distinct random ASCII strings of at least 32 characters")
		}
		seen[sum] = true
		return sum, nil
	}
	if !demo || config.Review != "" {
		access.review, err = add(config.Review)
		if err != nil {
			return Access{}, err
		}
	}
	for id, secret := range config.Spaces {
		if !graph.ValidID(id) {
			return Access{}, fmt.Errorf("invalid space ID in access file")
		}
		read, err := add(secret.Read)
		if err != nil {
			return Access{}, err
		}
		entry := spaceAccess{read: read}
		if secret.Propose != "" {
			entry.propose, err = add(secret.Propose)
			if err != nil {
				return Access{}, err
			}
		}
		access.spaces[id] = entry
	}
	return access, nil
}

func (a Access) SpaceIDs() []string {
	ids := make([]string, 0, len(a.spaces))
	for id := range a.spaces {
		ids = append(ids, id)
	}
	return ids
}

func (t tokenHash) matches(r *http.Request) bool {
	if t == (tokenHash{}) || len(r.Header.Values("Authorization")) != 1 {
		return false
	}
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return false
	}
	provided := sha256.Sum256([]byte(fields[1]))
	return subtle.ConstantTimeCompare(provided[:], t[:]) == 1
}

func localRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	// Both Host and Origin matter for browsers reaching a loopback listener.
	if host != "127.0.0.1" && host != "localhost" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
		return false
	}
	site := r.Header.Get("Sec-Fetch-Site")
	return site != "cross-site" && site != "same-site"
}

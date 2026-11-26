package cedar

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cedar-policy/cedar-go/types"
)

// Format is how a mapped token claim encodes a set of strings.
type Format string

// Supported claim formats.
const (
	// FormatStringArray is a JSON array of strings, e.g. "groups": ["a", "b"].
	FormatStringArray Format = "string-array"
	// FormatSpaceDelimited is one string of space-separated values, e.g. the
	// OAuth "scope" claim "read write".
	FormatSpaceDelimited Format = "space-delimited"
)

// Principal attribute names in the schema. Token claims with these names are
// rejected so they can never be confused with the derived attributes.
const (
	attrAuthenticated = "authenticated"
	attrIssuer        = "issuer"
	attrSubject       = "subject"
)

// AnonymousPrincipal is the principal ID of an unauthenticated caller.
const AnonymousPrincipal = "anonymous"

// Bounds on identity strings and mapped claim values.
const (
	maxIdentityString = 4096
	maxClaimValues    = 256
	maxClaimBytes     = 16 << 10
	maxClaimName      = 128
)

// AttributeMapping names the token claim that supplies one principal attribute,
// and its format.
type AttributeMapping struct {
	Claim  string
	Format Format
}

// Mapping selects the claims that become the principal's groups and scopes for
// tokens from one issuer. It applies only when the token's issuer equals Issuer
// exactly. A nil Groups or Scopes leaves that attribute absent.
type Mapping struct {
	Issuer string
	Groups *AttributeMapping
	Scopes *AttributeMapping
}

// Identity is the verified caller the host's authentication produced. The
// Authorizer trusts it; it does not verify tokens itself.
type Identity struct {
	// Anonymous is true when the request carried no credentials and the host
	// chose to let it through unauthenticated.
	Anonymous bool
	// Issuer and Subject must equal the token's iss and sub claims.
	Issuer, Subject string
	// Claims are the verified token claims; nil when anonymous.
	Claims map[string]any
}

// ValidateMappings checks mappings against the configured OAuth issuers. Each
// mapping's issuer must be in issuers, and at most one mapping may name an
// issuer. Claim names must be 1–128 characters with no surrounding whitespace
// and must not be a reserved name (iss, sub, authenticated, issuer, subject).
// Formats must be [FormatStringArray] or [FormatSpaceDelimited].
func ValidateMappings(issuers []string, mappings []Mapping) error {
	configured := make(map[string]bool, len(issuers))
	for _, issuer := range issuers {
		configured[issuer] = true
	}
	seen := make(map[string]bool, len(mappings))
	for i, m := range mappings {
		if m.Issuer == "" || !configured[m.Issuer] {
			return fmt.Errorf("cedar: identity mapping %d: issuer is not a configured OAuth issuer", i)
		}
		if seen[m.Issuer] {
			return fmt.Errorf("cedar: identity mapping %d: issuer already has a mapping", i)
		}
		seen[m.Issuer] = true
		for _, attr := range []struct {
			name    string
			mapping *AttributeMapping
		}{{"groups", m.Groups}, {"scopes", m.Scopes}} {
			if err := validateAttributeMapping(attr.mapping); err != nil {
				return fmt.Errorf("cedar: identity mapping %d: %s: %w", i, attr.name, err)
			}
		}
	}
	return nil
}

// PrincipalID returns the Cedar principal ID for an authenticated caller:
// "authenticated:" followed by the hex SHA-256 of the JSON array [issuer,
// subject]. The JSON encoding prevents delimiter collisions between issuers and
// subjects. Hosts can log it to correlate with [Decision].Principal. It returns
// "" when issuer or subject is empty, longer than 4096 bytes, not valid UTF-8,
// or contains NUL, CR or LF.
func PrincipalID(issuer, subject string) string {
	if !validString(issuer) || !validString(subject) {
		return ""
	}
	tuple, err := json.Marshal([]string{issuer, subject})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(tuple)
	return "authenticated:" + hex.EncodeToString(sum[:])
}

func validateAttributeMapping(a *AttributeMapping) error {
	if a == nil {
		return nil
	}
	if a.Claim == "" || len(a.Claim) > maxClaimName || strings.TrimSpace(a.Claim) != a.Claim {
		return errors.New("claim name must be 1-128 characters with no surrounding whitespace")
	}
	if reservedClaim(a.Claim) {
		return errors.New("claim name is reserved")
	}
	if a.Format != FormatStringArray && a.Format != FormatSpaceDelimited {
		return errors.New("unknown claim format")
	}
	return nil
}

func reservedClaim(name string) bool {
	switch name {
	case "iss", "sub", attrAuthenticated, attrIssuer, attrSubject:
		return true
	}
	return false
}

// principal builds the Cedar principal entity for identity. It returns
// ErrInvalidIdentity when the identity is inconsistent or a mapped claim is
// malformed. A mapped claim that is absent leaves its attribute absent.
func principal(identity Identity, mappings []Mapping) (types.Entity, error) {
	attrs := types.RecordMap{
		attrAuthenticated: types.Boolean(!identity.Anonymous),
		attrIssuer:        types.String(identity.Issuer),
		attrSubject:       types.String(identity.Subject),
	}
	uid := types.EntityUID{Type: principalType, ID: AnonymousPrincipal}
	if !identity.Anonymous {
		id := PrincipalID(identity.Issuer, identity.Subject)
		if id == "" || !claimEquals(identity.Claims, "iss", identity.Issuer) ||
			!claimEquals(identity.Claims, "sub", identity.Subject) {
			return types.Entity{}, ErrInvalidIdentity
		}
		for _, key := range []string{attrAuthenticated, attrIssuer, attrSubject} {
			if _, exists := identity.Claims[key]; exists {
				return types.Entity{}, ErrInvalidIdentity
			}
		}
		uid.ID = types.String(id)
		if err := mapAttributes(attrs, identity, mappings); err != nil {
			return types.Entity{}, err
		}
	}
	return types.Entity{UID: uid, Attributes: types.NewRecord(attrs)}, nil
}

func claimEquals(claims map[string]any, key, want string) bool {
	got, ok := claims[key].(string)
	return ok && got == want
}

func mapAttributes(attrs types.RecordMap, identity Identity, mappings []Mapping) error {
	for _, mapping := range mappings {
		if mapping.Issuer != identity.Issuer {
			continue
		}
		for _, mapped := range []struct {
			name    types.String
			mapping *AttributeMapping
		}{{"groups", mapping.Groups}, {"scopes", mapping.Scopes}} {
			if mapped.mapping == nil {
				continue
			}
			raw, exists := identity.Claims[mapped.mapping.Claim]
			if !exists {
				continue
			}
			values, err := attributeSet(raw, mapped.mapping.Format)
			if err != nil {
				return err
			}
			attrs[mapped.name] = values
		}
	}
	return nil
}

func validString(value string) bool {
	return value != "" && len(value) <= maxIdentityString && utf8.ValidString(value) &&
		!strings.ContainsAny(value, "\x00\r\n")
}

func attributeSet(raw any, format Format) (types.Set, error) {
	var values []string
	switch format {
	case FormatStringArray:
		var err error
		if values, err = stringArray(raw); err != nil {
			return types.Set{}, err
		}
	case FormatSpaceDelimited:
		s, ok := raw.(string)
		if !ok || len(s) > maxClaimBytes || !utf8.ValidString(s) || strings.ContainsAny(s, "\t\r\n\x00") {
			return types.Set{}, ErrInvalidIdentity
		}
		if s != "" {
			values = strings.Split(s, " ")
		}
	default:
		return types.Set{}, ErrInvalidIdentity
	}
	if len(values) > maxClaimValues {
		return types.Set{}, ErrInvalidIdentity
	}
	out := make([]types.Value, 0, len(values))
	total := 0
	for _, s := range values {
		total += len(s)
		if !validString(s) || total > maxClaimBytes {
			return types.Set{}, ErrInvalidIdentity
		}
		out = append(out, types.String(s))
	}
	return types.NewSet(out...), nil
}

func stringArray(raw any) ([]string, error) {
	switch v := raw.(type) {
	case []string:
		return v, nil
	case []any:
		if len(v) > maxClaimValues {
			return nil, ErrInvalidIdentity
		}
		values := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, ErrInvalidIdentity
			}
			values = append(values, s)
		}
		return values, nil
	default:
		return nil, ErrInvalidIdentity
	}
}

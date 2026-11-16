package authorization

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/cedar-policy/cedar-go/types"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

func principal(identity auth.VerifiedIdentity, mappings []config.IdentityMapping) (types.Entity, error) {
	attrs := types.RecordMap{
		"authenticated": types.Boolean(!identity.Anonymous),
		"issuer":        types.String(identity.Issuer), "subject": types.String(identity.Subject),
	}
	uid := types.EntityUID{Type: "Registry::Principal", ID: "anonymous"}
	if !identity.Anonymous {
		if !validString(identity.Issuer) || !validString(identity.Subject) ||
			identity.Claims["iss"] != identity.Issuer || identity.Claims["sub"] != identity.Subject {
			return types.Entity{}, errors.New("invalid verified identity")
		}
		for _, key := range []string{"authenticated", "issuer", "subject"} {
			if _, exists := identity.Claims[key]; exists {
				return types.Entity{}, errors.New("reserved identity attribute")
			}
		}
		uid.ID = types.String(auth.PrincipalID(identity))
		for _, mapping := range mappings {
			if mapping.Issuer != identity.Issuer {
				continue
			}
			for _, mapped := range []struct {
				name    types.String
				mapping *config.AttributeMapping
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
					return types.Entity{}, err
				}
				attrs[mapped.name] = values
			}
		}
	}
	return types.Entity{UID: uid, Attributes: types.NewRecord(attrs)}, nil
}

func validString(value string) bool {
	return value != "" && len(value) <= 4096 && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}

func attributeSet(raw any, format string) (types.Set, error) {
	var stringsIn []string
	switch format {
	case "string-array":
		var err error
		stringsIn, err = stringArray(raw)
		if err != nil {
			return types.Set{}, err
		}
	case "space-delimited":
		s, ok := raw.(string)
		if !ok || len(s) > 16384 || !utf8.ValidString(s) || strings.ContainsAny(s, "\t\r\n\x00") {
			return types.Set{}, errors.New("mapped claim has invalid type or size")
		}
		if s != "" {
			stringsIn = strings.Split(s, " ")
		}
	default:
		return types.Set{}, errors.New("unknown claim format")
	}
	if len(stringsIn) > 256 {
		return types.Set{}, errors.New("mapped claim exceeds bounds")
	}
	values := make([]types.Value, 0, len(stringsIn))
	total := 0
	for _, s := range stringsIn {
		total += len(s)
		if !validString(s) || total > 16384 {
			return types.Set{}, errors.New("mapped claim exceeds bounds")
		}
		values = append(values, types.String(s))
	}
	return types.NewSet(values...), nil
}

func stringArray(raw any) ([]string, error) {
	switch v := raw.(type) {
	case []string:
		return v, nil
	case []any:
		if len(v) > 256 {
			return nil, errors.New("mapped claim exceeds bounds")
		}
		values := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, errors.New("mapped claim has invalid type")
			}
			values = append(values, s)
		}
		return values, nil
	default:
		return nil, errors.New("mapped claim has invalid type")
	}
}

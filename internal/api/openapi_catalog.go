//go:build ignore

package api

// @Summary Retired identity endpoint
// @Description Requires OAuth authentication when admin is enabled. No legacy roles or claims are returned.
// @Security BearerAuth
// @Produce plain
// @Failure 401,410 {string} string
// @Router /v1/me [get]
func docRetiredMe() {}

// @Summary Retired entry claims endpoint
// @Description Authenticated static tombstone when admin is enabled. Claims are not read or mutated.
// @Security BearerAuth
// @Produce plain
// @Param type path string true "Entry kind"
// @Param name path string true "Entry name"
// @Failure 401,410 {string} string
// @Router /v1/entries/{type}/{name}/claims [get]
// @Router /v1/entries/{type}/{name}/claims [put]
func docRetiredClaims() {}

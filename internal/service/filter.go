package service

import "context"

// RecordFilter is a predicate applied to records after they are fetched from
// the database. Return true to keep the record, false to drop it.
// An error aborts the listing operation.
type RecordFilter func(ctx context.Context, record any) (bool, error)

// Package pgtypes provides custom types for PostgreSQL database operations.
// It includes types for handling PostgreSQL-specific data types that need
// special conversion to/from Go types.
package pgtypes

import (
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Interval represents a PostgreSQL INTERVAL type that maps to Go's time.Duration.
// This type implements sql.Scanner and driver.Valuer interfaces to allow seamless
// conversion between PostgreSQL INTERVAL and Go time.Duration.
type Interval struct {
	// Duration is the Go time.Duration representation
	Duration time.Duration
	// Valid indicates whether the interval is NULL
	Valid bool
}

// NewInterval creates a new Interval from a time.Duration
func NewInterval(d time.Duration) Interval {
	return Interval{
		Duration: d,
		Valid:    true,
	}
}

// NewNullInterval creates a NULL interval
func NewNullInterval() Interval {
	return Interval{
		Valid: false,
	}
}

// Scan implements the sql.Scanner interface to read PostgreSQL INTERVAL values
func (i *Interval) Scan(src any) error {
	if src == nil {
		i.Valid = false
		i.Duration = 0
		return nil
	}

	// pgx returns pgtype.Interval for INTERVAL columns
	switch v := src.(type) {
	case pgtype.Interval:
		// Convert pgtype.Interval to time.Duration
		// pgtype.Interval has Microseconds, Days, and Months fields
		// For sync schedules, we primarily care about hours/minutes/seconds
		// Days and months are converted to approximate durations
		microseconds := v.Microseconds
		microseconds += int64(v.Days) * 24 * 60 * 60 * 1000000        // days to microseconds
		microseconds += int64(v.Months) * 30 * 24 * 60 * 60 * 1000000 // months to microseconds (approximate)

		i.Duration = time.Duration(microseconds) * time.Microsecond
		i.Valid = v.Valid
		return nil
	case string:
		// Handle string representation of interval from database
		// Parse the string using pgtype.Interval's parser
		var pgInterval pgtype.Interval
		if err := pgInterval.Scan(v); err != nil {
			return fmt.Errorf("failed to parse interval string %q: %w", v, err)
		}

		// Convert to our Interval type using the pgtype.Interval case above
		microseconds := pgInterval.Microseconds
		microseconds += int64(pgInterval.Days) * 24 * 60 * 60 * 1000000
		microseconds += int64(pgInterval.Months) * 30 * 24 * 60 * 60 * 1000000

		i.Duration = time.Duration(microseconds) * time.Microsecond
		i.Valid = pgInterval.Valid
		return nil
	case []byte:
		// Handle byte slice (some drivers return intervals as []byte)
		return i.Scan(string(v))
	default:
		return fmt.Errorf("cannot scan %T into Interval", src)
	}
}

// Value implements the driver.Valuer interface to write PostgreSQL INTERVAL values
func (i Interval) Value() (driver.Value, error) {
	if !i.Valid {
		return nil, nil
	}

	// Convert time.Duration to pgtype.Interval
	// For simplicity, we store everything in microseconds
	// PostgreSQL will handle the normalization
	microseconds := i.Duration.Microseconds()

	return pgtype.Interval{
		Microseconds: microseconds,
		Days:         0,
		Months:       0,
		Valid:        true,
	}, nil
}

// String returns a human-readable representation of the interval
func (i Interval) String() string {
	if !i.Valid {
		return "NULL"
	}
	return i.Duration.String()
}

// ParseDuration parses a duration string (like "30m", "1h") and returns an Interval
func ParseDuration(s string) (Interval, error) {
	if s == "" {
		return NewNullInterval(), nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return Interval{}, err
	}

	return NewInterval(d), nil
}

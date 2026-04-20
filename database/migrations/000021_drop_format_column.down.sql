-- Restore the format column on source table.

ALTER TABLE source ADD COLUMN format TEXT;
ALTER TABLE source ADD CONSTRAINT registry_format_check
    CHECK (format IS NULL OR format IN ('toolhive', 'upstream'));

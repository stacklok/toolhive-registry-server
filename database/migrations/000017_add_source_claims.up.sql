-- Add claims column to source table for authorization claim inheritance
ALTER TABLE source ADD COLUMN claims JSONB;

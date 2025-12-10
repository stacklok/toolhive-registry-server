-- Create enum type for creation_type
CREATE TYPE creation_type AS ENUM (
    'API',
    'CONFIG'
);

-- Add creation_type column to registry table
ALTER TABLE registry
ADD COLUMN creation_type creation_type NOT NULL DEFAULT 'CONFIG';

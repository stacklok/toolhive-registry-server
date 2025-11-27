# sqlc Schema Directory

This directory contains table schemas used **ONLY for sqlc query validation**.

## Purpose

These schemas are NOT database migrations and will NOT be executed.

They exist solely to provide table definitions for sqlc to validate queries against temp tables that are created at runtime by application code.

## Files

- `temp_tables.sql` - Schemas for temporary tables used in bulk sync operations

## Important

⚠️ **DO NOT** add these schemas to the migrations directory.
⚠️ **DO NOT** run these schemas as migrations.
⚠️ These are for sqlc validation only.

-- View listings walk one kind's entries in name order, then each entry's
-- versions, and stop at the page size. This index lets that walk start at the
-- requested kind and cursor instead of ranking every entry in the view. It uses
-- the database's default collation because consumer lists sort names that way,
-- as v1.5.4 did; a COLLATE "C" index could not serve that order.
CREATE INDEX registry_entry_type_name_idx
    ON registry_entry (entry_type, name);

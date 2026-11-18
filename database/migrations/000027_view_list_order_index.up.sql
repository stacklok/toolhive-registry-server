-- View listings walk one kind's entries in byte-wise name order, then each
-- entry's versions, and stop at the page size. This index lets that walk start
-- at the requested kind and cursor instead of ranking every entry in the view.
CREATE INDEX registry_entry_type_name_c_idx
    ON registry_entry (entry_type, name COLLATE "C");

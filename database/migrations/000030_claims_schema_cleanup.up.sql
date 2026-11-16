-- Structural cleanup only: this migration never grants exposure consent.
BEGIN;
LOCK TABLE public.source, public.registry_sync, public.registry,
 public.registry_source, public.registry_entry, public.entry_version,
 public.mcp_server, public.mcp_server_package, public.mcp_server_remote, public.mcp_server_icon,
 public.skill, public.skill_oci_package, public.skill_git_package,
 public.plugin, public.plugin_oci_package, public.plugin_git_package, public.latest_entry_version,
 public.exposure_acknowledgement, public.exposure_claims_archive IN ACCESS EXCLUSIVE MODE;
DO $$
DECLARE
    receipt public.exposure_acknowledgement%ROWTYPE;
    catalog_empty BOOLEAN;
    current TEXT;
BEGIN
    SELECT * INTO STRICT receipt FROM public.exposure_acknowledgement WHERE singleton;
    IF EXISTS (SELECT 1 FROM public.source WHERE claims IS NOT NULL)
       OR EXISTS (SELECT 1 FROM public.registry WHERE claims IS NOT NULL)
       OR EXISTS (SELECT 1 FROM public.registry_entry WHERE claims IS NOT NULL) THEN
        RAISE EXCEPTION 'live claims require offline review, retirement and finalize at schema 29';
    END IF;
    IF EXISTS (SELECT 1 FROM public.registry_sync
               WHERE lease_id IS NOT NULL AND lease_expires_at > clock_timestamp()) THEN
        RAISE EXCEPTION 'drain every live lease, including stale generations, before cleanup';
    END IF;
    SELECT NOT (EXISTS (SELECT 1 FROM public.source)
        OR EXISTS (SELECT 1 FROM public.registry_sync)
        OR EXISTS (SELECT 1 FROM public.registry)
        OR EXISTS (SELECT 1 FROM public.registry_source)
        OR EXISTS (SELECT 1 FROM public.registry_entry)
        OR EXISTS (SELECT 1 FROM public.entry_version)
        OR EXISTS (SELECT 1 FROM public.mcp_server)
        OR EXISTS (SELECT 1 FROM public.mcp_server_package)
        OR EXISTS (SELECT 1 FROM public.mcp_server_remote)
        OR EXISTS (SELECT 1 FROM public.mcp_server_icon)
        OR EXISTS (SELECT 1 FROM public.skill)
        OR EXISTS (SELECT 1 FROM public.skill_oci_package)
        OR EXISTS (SELECT 1 FROM public.skill_git_package)
        OR EXISTS (SELECT 1 FROM public.plugin)
        OR EXISTS (SELECT 1 FROM public.plugin_oci_package)
        OR EXISTS (SELECT 1 FROM public.plugin_git_package)
        OR EXISTS (SELECT 1 FROM public.latest_entry_version)) INTO catalog_empty;
    IF receipt.consumed_at IS NULL THEN
        IF NOT catalog_empty OR receipt.acknowledgement_id IS NOT NULL
           OR receipt.retirement_id IS NOT NULL
           OR EXISTS (SELECT 1 FROM public.exposure_claims_archive) THEN
            RAISE EXCEPTION 'finalize exposure offline at schema 29 before cleanup';
        END IF;
    ELSIF receipt.acknowledgement_id IS NULL
       OR receipt.inventory_version <> 1
       OR receipt.reviewed_digest !~ '^sha256:[0-9a-f]{64}$'
       OR receipt.post_fence_digest !~ '^sha256:[0-9a-f]{64}$'
       OR receipt.config_revision !~ '^sha256:[0-9a-f]{64}$'
       OR receipt.consumed_target_digest IS NULL
       OR receipt.consumed_target_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT (receipt.acknowledgement_mode = 'empty'
           AND receipt.retirement_id IS NULL
           OR receipt.acknowledgement_mode = 'whole-catalog'
           AND receipt.retirement_id IS NOT NULL
           AND receipt.post_retirement_digest ~ '^sha256:[0-9a-f]{64}$') THEN
        RAISE EXCEPTION 'valid consumed exposure evidence required before cleanup';
    END IF;
    current := NULLIF(current_setting('toolhive.exposure_target_digest', true), '');
    IF receipt.consumed_at IS NOT NULL AND
       (current IS NULL OR current <> receipt.consumed_target_digest) THEN
        RAISE EXCEPTION 'consumed exposure target mismatch or verification context absent';
    END IF;
END $$;
-- End eligibility: the runner checks this block before marking version 30 dirty.
ALTER TABLE public.source DROP COLUMN claims;
ALTER TABLE public.registry DROP COLUMN claims;
ALTER TABLE public.registry_entry DROP COLUMN claims;
ALTER TABLE public.exposure_acknowledgement
    DROP CONSTRAINT exposure_acknowledgement_inventory_version_check,
    ADD CONSTRAINT exposure_acknowledgement_inventory_version_check CHECK (inventory_version IN (1, 2)),
    ALTER COLUMN inventory_version SET DEFAULT 2;
-- Preserve historical receipts verbatim. Only a virgin identity changes format.
UPDATE public.exposure_acknowledgement SET inventory_version = 2
WHERE acknowledgement_id IS NULL;
COMMIT;

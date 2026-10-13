# Rejected historical configuration fixtures

`config-docker-claims-test.yaml` deliberately retains retired resource claims for
offline migration characterization. `LoadCatalogConfig` may decode it for inspection,
but `ValidateCatalogCutover` and `serve` must reject it. It is not a deployment example
and must not be selected by Compose or Helm. Existing catalogs require the manual
inventory/acknowledgement/retirement procedure, even when their claims are SQL NULL.

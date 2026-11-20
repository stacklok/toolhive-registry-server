use cedar_policy::{
    Authorizer, Context, Entities, EntityUid, PolicySet, Request, Schema, ValidationMode, Validator,
};
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{env, fs, str::FromStr};

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let root = env::args().nth(1).ok_or("pass repository root")?;
    let schema_text = fs::read_to_string(format!("{root}/pkg/registry/cedar/schema.cedarschema"))?;
    let (schema, warnings) = Schema::from_cedarschema_str(&schema_text)?;
    assert_eq!(warnings.count(), 0);
    let cases_text = fs::read_to_string(format!(
        "{root}/pkg/registry/cedar/testdata/reference/cases.json"
    ))?;
    let document: Value = serde_json::from_str(&cases_text)?;
    let policies_by_name = document["policies"].as_object().ok_or("policies")?;
    let inputs = document["cases"].as_array().ok_or("cases")?;
    let validator = Validator::new(schema.clone());
    let mut results = Vec::new();
    for input in inputs {
        let name = input["policy"].as_str().ok_or("policy")?;
        let text = policies_by_name
            .get(name)
            .and_then(Value::as_str)
            .ok_or("unknown policy name")?;
        let policies = PolicySet::from_str(text)?;
        let validation = validator.validate(&policies, ValidationMode::Strict);
        let valid = validation.validation_passed();
        let principal = EntityUid::from_str("Registry::Principal::\"fixture\"")?;
        let (resource_type, resource_id) = if input["action"] == "catalog.read" {
            ("Registry::View", input["view"].as_str().unwrap_or("engineering"))
        } else {
            ("Registry::Instance", "catalog")
        };
        let resource = EntityUid::from_str(&format!("{resource_type}::\"{resource_id}\""))?;
        let action = EntityUid::from_str(&format!(
            "Registry::Action::\"{}\"",
            input["action"].as_str().ok_or("action")?
        ))?;
        let entities = Entities::from_json_value(
            json!([
                {"uid": {"type": "Registry::Principal", "id": "fixture"}, "attrs": input["attributes"], "parents": []},
                {"uid": {"type": resource_type, "id": resource_id}, "attrs": {}, "parents": []}
            ]),
            Some(&schema),
        )?;
        let request = Request::new(principal, action, resource, Context::empty(), Some(&schema))?;
        let response = Authorizer::new().is_authorized(&request, &policies, &entities);
        results.push(json!({"name": input["name"], "valid": valid,
            "decision": format!("{:?}", response.decision()), "errors": response.diagnostics().errors().count()}));
    }
    println!(
        "{}",
        serde_json::to_string_pretty(&json!({"reference": "cedar-policy 4.13.0",
        "schema_sha256": format!("{:x}", Sha256::digest(schema_text.as_bytes())),
        "cases_sha256": format!("{:x}", Sha256::digest(cases_text.as_bytes())),
        "results": results}))?
    );
    Ok(())
}

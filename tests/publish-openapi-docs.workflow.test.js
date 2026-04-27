const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const yaml = require("js-yaml");

const workflowPath = path.join(__dirname, "..", ".github", "workflows", "publish-openapi-docs.yml");
const workflow = yaml.load(fs.readFileSync(workflowPath, "utf-8"));

test("publish-openapi-docs workflow publishes filtered OpenAPI bundles to each subscriber", () => {
  const uploadArtifactStep = workflow.jobs["build-openapi"].steps.find(
    (step) => step.name === "Upload OpenAPI build artifact",
  );
  assert.match(uploadArtifactStep.with.path, /_openapi_build\/cloud_openapi\.yml/);
  assert.match(uploadArtifactStep.with.path, /_openapi_build\/meshery_openapi\.yml/);
  assert.doesNotMatch(uploadArtifactStep.with.path, /merged_openapi\.yml/);

  const mesheryCopyStep = workflow.jobs["publish-openapi-docs"].steps.find(
    (step) => step.name === "Copy OpenAPI specification to meshery repo",
  );
  assert.match(mesheryCopyStep.run, /cp meshery_openapi\.yml meshery\/docs\/data\/openapi\.yml/);
  assert.doesNotMatch(mesheryCopyStep.run, /merged_openapi\.yml/);

  const cloudCopyStep = workflow.jobs["publish-openapi-docs-to-cloud-docs"].steps.find(
    (step) => step.name === "Copy OpenAPI specification to repo",
  );
  assert.match(cloudCopyStep.run, /cp cloud_openapi\.yml cloud-docs\/data\/openapi\.yml/);
  assert.doesNotMatch(cloudCopyStep.run, /merged_openapi\.yml/);
});

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const yaml = require("js-yaml");

function readWorkflow(name) {
  const workflowPath = path.join(__dirname, "..", ".github", "workflows", name);
  return yaml.load(fs.readFileSync(workflowPath, "utf-8"));
}

function getTriggers(workflow) {
  return workflow.on ?? {};
}

function hasTrigger(triggers, name) {
  return Object.prototype.hasOwnProperty.call(triggers, name);
}

test("publish-schemas workflow updates schema versions before dispatching downstream release workflows", () => {
  const workflow = readWorkflow("publish-schemas.yml");
  const triggers = getTriggers(workflow);

  assert.deepEqual(triggers.release.types, ["published"]);

  const updateJob = workflow.jobs["update-schema-version"];
  assert.ok(updateJob, "expected update-schema-version job to exist");

  const updateStep = updateJob.steps.find((step) => step.name === "Update schema versions");
  assert.ok(updateStep, "expected Update schema versions step to exist");
  assert.match(updateStep.run, /schemas\/base_cloud\.yml/);
  assert.match(updateStep.run, /schemas\/base_meshery\.yml/);
  assert.equal(updateStep.env.RELEASE_VERSION, "${{ github.event.release.tag_name }}");

  assert.equal(workflow.jobs["publish-npm-package"].needs, "update-schema-version");
  assert.equal(workflow.jobs["notify-dependents"].needs, "publish-npm-package");
  assert.equal(workflow.jobs["publish-openapi-docs"].needs, "publish-npm-package");
  assert.equal(workflow.jobs["publish-npm-package"].uses, "./.github/workflows/publish-npm-package.yml");
  assert.equal(workflow.jobs["notify-dependents"].uses, "./.github/workflows/notify-dependents.yml");
  assert.equal(workflow.jobs["publish-openapi-docs"].uses, "./.github/workflows/publish-openapi-docs.yml");
  assert.equal(workflow.jobs["publish-openapi-docs"].secrets, "inherit");
  assert.equal(workflow.jobs["publish-npm-package"].with.release_version, "${{ github.event.release.tag_name }}");
  assert.equal(workflow.jobs["notify-dependents"].with.release_version, "${{ github.event.release.tag_name }}");
});

test("release workflows are reusable and manually dispatchable without direct published triggers", () => {
  for (const name of [
    "publish-npm-package.yml",
    "notify-dependents.yml",
    "publish-openapi-docs.yml",
  ]) {
    const workflow = readWorkflow(name);
    const triggers = getTriggers(workflow);

    assert.equal(hasTrigger(triggers, "workflow_call"), true, `${name} should support workflow_call`);
    assert.equal(hasTrigger(triggers, "workflow_dispatch"), true, `${name} should support workflow_dispatch`);
    assert.equal(triggers.release, undefined, `${name} should not listen for release events directly`);
  }
});

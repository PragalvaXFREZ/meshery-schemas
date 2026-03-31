/**
 * config.js - Shared Configuration for Build Scripts
 *
 * Dynamically discovers schema packages by walking the schemas/constructs directory.
 * Looks for directories containing an api.yml file (the index file for each construct).
 *
 * The api.yml file serves as the index for each construct:
 * - References all subschemas via $ref
 * - Defines all API endpoints for the construct
 * - Acts as the entry point for code generation tools
 *
 * Special cases and merge configuration can be defined in the overrides section.
 */

const fs = require("fs");
const path = require("path");

/**
 * Build paths configuration
 */
const paths = {
  // Root directories (relative to project root)
  schemasDir: "schemas/constructs",
  buildDir: "_openapi_build",
  modelsDir: "models",
  typescriptDir: "typescript/generated",

  // Build config files
  openapiConfig: "build/openapi.config.yml",
  baseCloudSpec: "schemas/base_cloud.yml",

  // Output files
  mergedOpenapi: "_openapi_build/merged_openapi.yml",
  cloudOpenapi: "_openapi_build/cloud_openapi.yml",
  mesheryOpenapi: "_openapi_build/meshery_openapi.yml",

  // RTK config files
  cloudRtkConfig: "typescript/rtk/cloud-rtk-config.ts",
  mesheryRtkConfig: "typescript/rtk/meshery-rtk-config.ts",
};

/**
 * Merged construct filename (must be JSON for RTK query generation)
 */
const mergedConstructFilename = "merged-openapi.json";

/**
 * Package name overrides
 * Maps directory name to a different package name for Go code generation
 * Key: "version/dirname", Value: package name
 */
const packageNameOverrides = {
  "v1beta1/design": "pattern",
};

/**
 * Packages to EXCLUDE from processing
 * These directories will be skipped even if they contain api.yml
 */
const excludePackages = [
  // Deprecated v1beta1/relationship has circular $ref aliases in api.yml.
  // Use v1alpha3 Go models instead.
  "v1beta1/relationship",
];

/**
 * Packages to EXCLUDE from Go code generation only.
 * These are still discovered and available for bundling/$ref resolution,
 * but oapi-codegen is not run on them.
 */
const excludeFromGoGeneration = [
  // Core is a bundled base schema. The v1alpha1/core Go package provides
  // the generated types. v1beta1/core and v1beta2/core schemas exist for
  // $ref resolution but don't need their own Go packages.
  "v1beta1/core",
  "v1beta2/core",
  // v1beta2/catalog: Go models live at models/v1alpha2/catalog (the
  // promoted source). Generating at models/v1beta2/catalog would create
  // a duplicate source of truth.
  "v1beta2/catalog",
];

/**
 * Packages to EXCLUDE from the merged OpenAPI spec
 * These will still be processed for Go generation but not included in the merge
 */
const excludeFromMerge = [
  // Promoted constructs (v1alpha* → v1beta1): original alpha directories
  // still exist for backward compatibility but are excluded from merge.
  "v1alpha1/core",
  "v1alpha1/capability",
  // Core, capability, selector are reusable base schemas, not standalone
  // API surfaces — exclude from the merged spec in all versions.
  "v1beta1/core",
  "v1beta1/capability",
  "v1beta1/selector",
  "v1beta2/core",
  "v1beta2/selector",
  // Deprecated v1beta1 constructs superseded by v1beta2.
  // Excluded to prevent path conflicts during OpenAPI merge.
  "v1beta1/academy",
  "v1beta1/catalog",
  "v1beta1/component",
  "v1beta1/connection",
  "v1beta1/design",
  "v1beta1/event",
  "v1beta1/invitation",
  "v1beta1/plan",
  "v1beta1/relationship",
  "v1beta1/subscription",
  "v1beta1/token",
];

/**
 * Get the project root directory
 * @returns {string} Absolute path to project root
 */
function getProjectRoot() {
  return path.resolve(__dirname, "../..");
}

/**
 * Discover all schema packages by walking the schemas directory
 * Looks for directories containing an api.yml file (the construct index file)
 *
 * @returns {Array<{name: string, version: string, dirName: string, openapiPath: string}>}
 */
function discoverSchemaPackages() {
  const projectRoot = getProjectRoot();
  const schemasRoot = path.join(projectRoot, paths.schemasDir);
  const packages = [];

  if (!fs.existsSync(schemasRoot)) {
    console.warn(`Schemas directory not found: ${schemasRoot}`);
    return packages;
  }

  // Get all version directories (sorted for deterministic discovery order)
  const versionDirs = fs.readdirSync(schemasRoot).filter((item) => {
    const itemPath = path.join(schemasRoot, item);
    return fs.statSync(itemPath).isDirectory();
  }).sort();

  for (const version of versionDirs) {
    const versionPath = path.join(schemasRoot, version);
    const packageDirs = fs.readdirSync(versionPath).filter((item) => {
      const itemPath = path.join(versionPath, item);
      return fs.statSync(itemPath).isDirectory();
    }).sort();

    for (const dirName of packageDirs) {
      const openapiPath = path.join(versionPath, dirName, "api.yml");
      const packageKey = `${version}/${dirName}`;

      // Skip excluded packages
      if (excludePackages.includes(packageKey)) {
        continue;
      }

      // Check if api.yml exists (the construct index file)
      if (fs.existsSync(openapiPath)) {
        // Get package name (use override if exists, otherwise use directory name)
        const packageName = packageNameOverrides[packageKey] || dirName;

        packages.push({
          name: packageName,
          version: version,
          dirName: dirName,
          openapiPath: path.relative(projectRoot, openapiPath),
        });
      }
    }
  }

  return packages;
}

/**
 * Get all schema packages (cached)
 * @returns {Array<{name: string, version: string, dirName: string, openapiPath: string}>}
 */
let _cachedPackages = null;
function getSchemaPackages() {
  if (_cachedPackages === null) {
    _cachedPackages = discoverSchemaPackages();
  }
  return _cachedPackages;
}

/**
 * Get packages that should be included in the merged OpenAPI spec
 * @returns {Array<{name: string, version: string, dirName: string, openapiPath: string}>}
 */
function getMergePackages() {
  return getSchemaPackages().filter((pkg) => {
    const packageKey = `${pkg.version}/${pkg.dirName}`;
    return !excludeFromMerge.includes(packageKey);
  });
}

/**
 * Get the input schema path for a package
 * @param {Object} pkg - Package definition
 * @returns {string} Path to input schema (relative to project root)
 */
function getInputSchemaPath(pkg) {
  return pkg.openapiPath;
}

/**
 * Get the bundled output path for a package
 * @param {Object} pkg - Package definition
 * @returns {string} Path to bundled output (relative to project root)
 */
function getBundledOutputPath(pkg) {
  return `${paths.buildDir}/constructs/${pkg.version}/${pkg.name}/${mergedConstructFilename}`;
}

/**
 * Get the Go output path for a package
 * @param {Object} pkg - Package definition
 * @returns {string} Path to Go output file (relative to project root)
 */
function getGoOutputPath(pkg) {
  return `${paths.modelsDir}/${pkg.version}/${pkg.name}/${pkg.name}.go`;
}

/**
 * Clear the cached packages (useful for testing)
 */
function clearCache() {
  _cachedPackages = null;
}

/**
 * Print discovered packages (for debugging)
 */
function printDiscoveredPackages() {
  const packages = getSchemaPackages();
  console.log(`\nDiscovered ${packages.length} schema packages:\n`);
  for (const pkg of packages) {
    const overridden = pkg.name !== pkg.dirName ? ` (from ${pkg.dirName})` : "";
    console.log(`  - ${pkg.version}/${pkg.name}${overridden}`);
  }
  console.log("");
}

module.exports = {
  paths,
  mergedConstructFilename,
  packageNameOverrides,
  excludePackages,
  excludeFromMerge,
  excludeFromGoGeneration,
  getProjectRoot,
  discoverSchemaPackages,
  getSchemaPackages,
  getMergePackages,
  getInputSchemaPath,
  getBundledOutputPath,
  getGoOutputPath,
  clearCache,
  printDiscoveredPackages,
};

#!/usr/bin/env node
/**
 * generate-golang.js - Go Code Generation Script
 *
 * DESCRIPTION:
 *   Generates Go structs from source OpenAPI specifications using oapi-codegen.
 *   For each package, the script builds a temporary oapi-codegen configuration
 *   with import mappings derived from reachable external $ref targets so shared
 *   types can be reused across generated Go packages.
 *
 *   Schemas are discovered dynamically by walking the schemas/constructs directory
 *   and looking for directories containing an api.yml file (the index file for each construct).
 *
 * WHAT IT DOES:
 *   1. Discovers all schema packages from schemas/constructs/
 *   2. Generates Go structs with JSON and YAML struct tags using oapi-codegen
 *   3. Outputs Go files to models/<version>/<package>/
 *
 * USAGE:
 *   node build/generate-golang.js
 *
 * DEPENDENCIES:
 *   - oapi-codegen (Go tool)
 *
 * OUTPUT:
 *   - models/<version>/<package>/<package>.go
 */

const fs = require("fs");
const os = require("os");
const path = require("path");
const { execSync } = require("child_process");
const yaml = require("js-yaml");
const logger = require("./lib/logger");
const config = require("./lib/config");
const paths = require("./lib/paths");
const { commandExists } = require("./lib/exec");

/**
 * Add YAML struct tags alongside JSON ones in generated Go file
 * @param {string} filePath - Path to Go file
 */
function addYamlTags(filePath) {
  let content = fs.readFileSync(filePath, "utf-8");

  // Add yaml struct tags matching the json tags
  // Pattern: json:"fieldName" -> json:"fieldName" yaml:"fieldName"
  content = content.replace(
    /json:"([^"]*)"(\s+yaml:"[^"]*")?/g,
    'json:"$1" yaml:"$1"',
  );

  fs.writeFileSync(filePath, content, "utf-8");
}

function collectSchemaExtraTags(inputPath) {
  const document = loadYamlFile(inputPath) || {};
  const componentSchemas = document.components?.schemas || {};
  const extraTagsByStruct = new Map();

  for (const [schemaName, schemaDefinition] of Object.entries(componentSchemas)) {
    if (!schemaDefinition || typeof schemaDefinition !== "object") {
      continue;
    }

    if (schemaDefinition.type !== "object" || !schemaDefinition.properties) {
      continue;
    }

    const propertyTags = new Map();

    for (const [propertyName, propertyDefinition] of Object.entries(
      schemaDefinition.properties,
    )) {
      if (!propertyDefinition || typeof propertyDefinition !== "object") {
        continue;
      }

      const extraTags = propertyDefinition["x-oapi-codegen-extra-tags"];
      if (!extraTags || typeof extraTags !== "object") {
        continue;
      }

      propertyTags.set(propertyName, { ...extraTags });
    }

    if (propertyTags.size > 0) {
      extraTagsByStruct.set(schemaName, propertyTags);
    }
  }

  return extraTagsByStruct;
}

function addSchemaExtraTags(filePath, inputPath) {
  const extraTagsByStruct = collectSchemaExtraTags(inputPath);
  if (extraTagsByStruct.size === 0) {
    return;
  }

  const lines = fs.readFileSync(filePath, "utf-8").split("\n");
  let currentStructName = null;
  let structDepth = 0;

  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    const structMatch = line.match(/^type\s+(\w+)\s+struct\s*\{$/);
    if (structMatch) {
      currentStructName = structMatch[1];
      structDepth = 1;
      continue;
    }

    if (!currentStructName) {
      continue;
    }

    if (structDepth === 1) {
      const fieldMatch = line.match(/^\s*\w+[^`]*`([^`]*)`/);
      if (fieldMatch) {
        const rawTags = fieldMatch[1];
        const jsonMatch = rawTags.match(/json:"([^",]+)(?:,[^"]*)?"/);
        if (jsonMatch) {
          const propertyName = jsonMatch[1];
          const propertyTags = extraTagsByStruct
            .get(currentStructName)
            ?.get(propertyName);

          if (propertyTags) {
            let updatedTags = rawTags;

            for (const [tagName, tagValue] of Object.entries(propertyTags)) {
              if (updatedTags.includes(`${tagName}:"`)) {
                continue;
              }

              const sanitizedValue = String(tagValue).replace(/"/g, '\\"');
              updatedTags = `${tagName}:"${sanitizedValue}" ${updatedTags}`;
            }

            if (updatedTags !== rawTags) {
              lines[index] = line.replace(`\`${rawTags}\``, `\`${updatedTags}\``);
            }
          }
        }
      }
    }

    const opens = (line.match(/struct\s*\{/g) || []).length;
    const closes = (line.match(/}/g) || []).length;
    structDepth += opens - closes;

    if (structDepth <= 0) {
      currentStructName = null;
      structDepth = 0;
    }
  }

  fs.writeFileSync(filePath, lines.join("\n"), "utf-8");
}

function toPosixPath(filePath) {
  return filePath.split(path.sep).join("/");
}

function splitRef(ref) {
  const hashIndex = ref.indexOf("#");
  if (hashIndex === -1) {
    return { refPath: ref, fragment: "" };
  }

  return {
    refPath: ref.slice(0, hashIndex),
    fragment: ref.slice(hashIndex + 1),
  };
}

function loadYamlFile(filePath) {
  try {
    return yaml.load(fs.readFileSync(filePath, "utf-8"));
  } catch (err) {
    throw new Error(`Failed to parse schema file ${filePath}: ${err.message}`);
  }
}

function collectRefs(node, refs = new Set()) {
  if (Array.isArray(node)) {
    for (const item of node) {
      collectRefs(item, refs);
    }
    return refs;
  }

  if (!node || typeof node !== "object") {
    return refs;
  }

  if (typeof node.$ref === "string") {
    refs.add(node.$ref);
  }

  for (const value of Object.values(node)) {
    collectRefs(value, refs);
  }

  return refs;
}

function getPackageRoot(pkg) {
  return path.dirname(paths.fromRoot(pkg.openapiPath));
}

function getPackageKey(pkg) {
  return `${pkg.version}/${pkg.dirName}`;
}

function getPackageImportPath(pkg) {
  return `github.com/meshery/schemas/models/${pkg.version}/${pkg.name}`;
}

function findPackageForFile(filePath) {
  const resolvedFile = fs.existsSync(filePath)
    ? fs.realpathSync(filePath)
    : path.resolve(filePath);
  const packages = config
    .getSchemaPackages()
    .map((pkg) => ({ ...pkg, root: getPackageRoot(pkg) }))
    .sort((left, right) => right.root.length - left.root.length);

  return (
    packages.find((pkg) => {
      const packageRoot = pkg.root;
      return (
        resolvedFile === packageRoot ||
        resolvedFile.startsWith(`${packageRoot}${path.sep}`)
      );
    }) || null
  );
}

const componentRefCache = new Map();

function findPackageComponentRef(targetPkg, targetFile, fragment = "") {
  const cacheKey = `${getPackageKey(targetPkg)}:${targetFile}#${fragment}`;
  if (componentRefCache.has(cacheKey)) {
    return componentRefCache.get(cacheKey);
  }

  const apiPath = paths.fromRoot(targetPkg.openapiPath);
  const apiDocument = loadYamlFile(apiPath) || {};
  const components = apiDocument.components || {};
  const matches = [];

  for (const [kind, definitions] of Object.entries(components)) {
    if (!definitions || typeof definitions !== "object") {
      continue;
    }

    for (const [name, definition] of Object.entries(definitions)) {
      if (!definition || typeof definition !== "object") {
        continue;
      }

      if (typeof definition.$ref !== "string" || definition.$ref.startsWith("#")) {
        continue;
      }

      const { refPath, fragment: componentFragment } = splitRef(definition.$ref);
      const resolvedComponentRef = path.resolve(path.dirname(apiPath), refPath);
      const resolvedTarget = fs.existsSync(targetFile)
        ? fs.realpathSync(targetFile)
        : path.resolve(targetFile);

      if (
        resolvedComponentRef === resolvedTarget &&
        componentFragment === fragment
      ) {
        matches.push({ kind, name });
      }
    }
  }

  if (matches.length > 1) {
    throw new Error(
      `Multiple component refs found for ${targetFile} in ${targetPkg.openapiPath}`,
    );
  }

  const match = matches[0] || null;
  componentRefCache.set(cacheKey, match);
  return match;
}

function transformRefs(node, transformRef) {
  if (Array.isArray(node)) {
    for (const item of node) {
      transformRefs(item, transformRef);
    }
    return;
  }

  if (!node || typeof node !== "object") {
    return;
  }

  if (typeof node.$ref === "string") {
    node.$ref = transformRef(node.$ref);
  }

  for (const value of Object.values(node)) {
    transformRefs(value, transformRef);
  }
}

function stagePackageSources(currentPkg, sourceInputPath) {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), `meshery-oapi-src-${currentPkg.name}-`));
  const tempConstructsRoot = path.join(tempDir, "schemas", "constructs");
  const realConstructsRoot = paths.fromRoot(config.paths.schemasDir);
  const versionDirs = fs.readdirSync(realConstructsRoot, { withFileTypes: true });

  for (const versionEntry of versionDirs) {
    if (!versionEntry.isDirectory()) {
      continue;
    }

    const versionName = versionEntry.name;
    const realVersionPath = path.join(realConstructsRoot, versionName);
    const tempVersionPath = path.join(tempConstructsRoot, versionName);
    fs.mkdirSync(tempVersionPath, { recursive: true });

    for (const entry of fs.readdirSync(realVersionPath, { withFileTypes: true })) {
      const realEntryPath = path.join(realVersionPath, entry.name);
      const tempEntryPath = path.join(tempVersionPath, entry.name);
      const isCurrentPackage =
        versionName === currentPkg.version && entry.name === currentPkg.dirName;

      if (isCurrentPackage) {
        fs.cpSync(realEntryPath, tempEntryPath, { recursive: true });
        continue;
      }

      fs.symlinkSync(realEntryPath, tempEntryPath);
    }
  }

  const stagedPackageRoot = path.join(
    tempConstructsRoot,
    currentPkg.version,
    currentPkg.dirName,
  );
  const stagedFiles = paths.findFiles(stagedPackageRoot, /\.(yml|yaml)$/);

  for (const stagedFile of stagedFiles) {
    const document = loadYamlFile(stagedFile);
    if (!document || typeof document !== "object") {
      continue;
    }

    transformRefs(document, (ref) => {
      if (typeof ref !== "string" || ref.startsWith("#") || /^https?:\/\//.test(ref)) {
        return ref;
      }

      const { refPath, fragment } = splitRef(ref);
      if (!refPath) {
        return ref;
      }

      const resolvedRef = path.resolve(path.dirname(stagedFile), refPath);
      const targetPkg = findPackageForFile(resolvedRef);
      if (!targetPkg || getPackageKey(targetPkg) === getPackageKey(currentPkg)) {
        return ref;
      }

      if (path.basename(resolvedRef) === "api.yml") {
        return ref;
      }

      const componentRef = findPackageComponentRef(targetPkg, resolvedRef, fragment);
      if (!componentRef) {
        return ref;
      }

      const targetApiPath = path.join(
        tempConstructsRoot,
        targetPkg.version,
        targetPkg.dirName,
        "api.yml",
      );
      const relativeApiPath = toPosixPath(
        path.relative(path.dirname(stagedFile), targetApiPath),
      );

      return `${relativeApiPath}#/components/${componentRef.kind}/${componentRef.name}`;
    });

    fs.writeFileSync(stagedFile, yaml.dump(document), "utf-8");
  }

  return {
    tempDir,
    inputPath: path.join(stagedPackageRoot, path.basename(sourceInputPath)),
  };
}

function resolveGoInputSchemaPath(pkg) {
  const defaultInputPath = paths.fromRoot(config.getInputSchemaPath(pkg));

  if (pkg.version === "v1alpha3" && pkg.name === "relationship") {
    const relationshipSourcePath = path.join(
      path.dirname(defaultInputPath),
      "relationship.yaml",
    );

    if (paths.fileExists(relationshipSourcePath)) {
      return relationshipSourcePath;
    }
  }

  return defaultInputPath;
}

function buildImportMappings(currentPkg, entryPath) {
  const currentPackageKey = getPackageKey(currentPkg);
  const currentPackageRoot = path.dirname(entryPath);
  const visitedFiles = new Set();
  const pendingFiles = [path.resolve(entryPath)];
  const importMappings = {};

  while (pendingFiles.length > 0) {
    const currentFile = pendingFiles.pop();
    if (visitedFiles.has(currentFile)) {
      continue;
    }

    visitedFiles.add(currentFile);
    const document = loadYamlFile(currentFile);
    const refs = collectRefs(document);

    for (const ref of refs) {
      if (typeof ref !== "string" || ref.startsWith("#")) {
        continue;
      }

      const [refPath] = ref.split("#", 1);
      if (!refPath || /^https?:\/\//.test(refPath)) {
        continue;
      }

      const resolvedRef = path.resolve(path.dirname(currentFile), refPath);
      if (!fs.existsSync(resolvedRef)) {
        throw new Error(
          `Unable to resolve external reference '${refPath}' from ${currentFile}`,
        );
      }

      const isCurrentPackageRef =
        resolvedRef === currentPackageRoot ||
        resolvedRef.startsWith(`${currentPackageRoot}${path.sep}`);

      if (isCurrentPackageRef) {
        importMappings[toPosixPath(refPath)] = "-";

        pendingFiles.push(resolvedRef);
        continue;
      }

      const targetPkg = findPackageForFile(resolvedRef);
      if (!targetPkg) {
        pendingFiles.push(resolvedRef);
        continue;
      }

      if (getPackageKey(targetPkg) === currentPackageKey) {
        importMappings[toPosixPath(refPath)] = "-";

        pendingFiles.push(resolvedRef);
        continue;
      }

      const normalizedRefPath = toPosixPath(refPath);
      const importPath = getPackageImportPath(targetPkg);
      const existingImportPath = importMappings[normalizedRefPath];

      if (existingImportPath && existingImportPath !== importPath) {
        throw new Error(
          `Conflicting import mappings for '${normalizedRefPath}': ` +
            `${existingImportPath} vs ${importPath}`,
        );
      }

      importMappings[normalizedRefPath] = importPath;
    }
  }

  return importMappings;
}

function createGeneratorConfig(pkg, inputPath, tempDir) {
  const baseConfigPath = paths.fromRoot(config.paths.openapiConfig);
  const baseConfig = loadYamlFile(baseConfigPath) || {};
  const tempConfigPath = path.join(tempDir, "openapi.config.yml");
  const generatedConfig = {
    ...baseConfig,
    package: pkg.name,
    "import-mapping": {
      ...(baseConfig["import-mapping"] || {}),
      ...buildImportMappings(pkg, inputPath),
    },
  };

  fs.writeFileSync(tempConfigPath, yaml.dump(generatedConfig), "utf-8");

  return { tempDir, tempConfigPath };
}

/**
 * Generate Go models for a single package
 * @param {Object} pkg - Package definition
 * @returns {Promise<void>}
 */
async function generateGoModels(pkg) {
  const outputPath = paths.fromRoot(config.getGoOutputPath(pkg));
  const sourceInputPath = resolveGoInputSchemaPath(pkg);

  // Verify input exists
  if (!paths.fileExists(sourceInputPath)) {
    logger.warn(`Schema not found: ${sourceInputPath}, skipping ${pkg.name}`);
    return;
  }

  // Ensure output directory exists
  paths.ensureParentDir(outputPath);

  logger.step(`Generating Go models: ${pkg.name} (${pkg.version})...`);

  let tempDir;
  try {
    const stagedSources = stagePackageSources(pkg, sourceInputPath);
    tempDir = stagedSources.tempDir;

    const inputPath = stagedSources.inputPath;
    const generatedConfig = createGeneratorConfig(pkg, inputPath, tempDir);

    execSync(
      `oapi-codegen --config "${generatedConfig.tempConfigPath}" ` +
        `--package "${pkg.name}" ` +
        `-generate types ` +
        `--include-tags all ` +
        `-o "${outputPath}" ` +
        `"${inputPath}"`,
      { stdio: "inherit" },
    );

    // Add YAML struct tags and restore any schema-declared extra tags that
    // oapi-codegen omits for referenced object fields.
    addYamlTags(outputPath);
    addSchemaExtraTags(outputPath, inputPath);

    logger.success(`Generated: ${paths.relativePath(outputPath)}`);
  } catch (err) {
    throw new Error(
      `Go model generation failed for ${pkg.name}: ${err.message}`,
    );
  } finally {
    if (tempDir) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }
  }
}

/**
 * Check prerequisites
 */
function checkPrerequisites() {
  // Check for oapi-codegen
  if (!commandExists("oapi-codegen")) {
    logger.error("oapi-codegen not found.");
    logger.info(
      "Install it with: go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest",
    );
    process.exit(1);
  }

}

/**
 * Main entry point
 */
async function main() {
  const startTime = Date.now();

  try {
    // Change to project root
    process.chdir(paths.getProjectRoot());

    // Add Go bin to PATH
    const goPath = process.env.GOPATH || `${process.env.HOME}/go`;
    process.env.PATH = `${goPath}/bin:${process.env.PATH}`;

    logger.header("🔧 Starting Go code generation...");

    // Check prerequisites
    checkPrerequisites();

    // Discover packages dynamically
    const packageFilter = process.env.SCHEMA_PACKAGE
      ? new Set(
          process.env.SCHEMA_PACKAGE.split(",")
            .map((value) => value.trim())
            .filter(Boolean),
        )
      : null;
    const schemaPackages = config.getSchemaPackages().filter((pkg) => {
      if (!packageFilter) {
        return true;
      }

      return (
        packageFilter.has(pkg.name) ||
        packageFilter.has(pkg.dirName) ||
        packageFilter.has(`${pkg.version}/${pkg.dirName}`)
      );
    });
    logger.info(`Discovered ${schemaPackages.length} schema packages`);

    if (schemaPackages.length === 0) {
      logger.error("No schema packages found!");
      process.exit(1);
    }

    // Generate Go models for all packages
    for (const pkg of schemaPackages) {
      await generateGoModels(pkg);
    }

    // Summary
    const duration = ((Date.now() - startTime) / 1000).toFixed(2);
    logger.blank();
    logger.success(`Go code generation complete! (${duration}s)`);
    logger.outputFiles(["models/<version>/<package>/*.go"]);
  } catch (err) {
    logger.error(err.message);
    process.exit(1);
  }
}

module.exports = {
  addYamlTags,
  buildImportMappings,
  collectRefs,
  createGeneratorConfig,
  findPackageForFile,
  generateGoModels,
  loadYamlFile,
  main,
  stagePackageSources,
};

if (require.main === module) {
  main();
}

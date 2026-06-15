#!/usr/bin/env node

/**
 * Build Lambda Layers Script
 * Creates optimized Lambda layers for dependencies and common code
 */

const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');

// Configuration
const ROOT_DIR = path.join(__dirname, '..');
const BUILD_DIR = path.join(ROOT_DIR, 'build');
const DEPENDENCIES_LAYER_DIR = path.join(BUILD_DIR, 'dependencies-layer');
const COMMON_LAYER_DIR = path.join(BUILD_DIR, 'common-layer');

// Colors for console output
const colors = {
  reset: '\x1b[0m',
  green: '\x1b[32m',
  blue: '\x1b[34m',
  yellow: '\x1b[33m',
  red: '\x1b[31m',
};

function log(message, color = 'reset') {
  console.log(`${colors[color]}${message}${colors.reset}`);
}

function execCommand(command, cwd = ROOT_DIR) {
  log(`Executing: ${command}`, 'blue');
  try {
    execSync(command, { cwd, stdio: 'inherit' });
  } catch (error) {
    log(`Error executing command: ${command}`, 'red');
    throw error;
  }
}

function cleanDirectory(dir) {
  if (fs.existsSync(dir)) {
    log(`Cleaning: ${path.basename(dir)}`, 'yellow');
    fs.rmSync(dir, { recursive: true, force: true });
  }
  fs.mkdirSync(dir, { recursive: true });
}

function copyDirectory(src, dest) {
  log(`Copying ${path.basename(src)}`, 'blue');
  fs.mkdirSync(dest, { recursive: true });

  const entries = fs.readdirSync(src, { withFileTypes: true });

  for (const entry of entries) {
    const srcPath = path.join(src, entry.name);
    const destPath = path.join(dest, entry.name);

    if (entry.isDirectory()) {
      copyDirectory(srcPath, destPath);
    } else {
      fs.copyFileSync(srcPath, destPath);
    }
  }
}

function buildDependenciesLayer() {
  log('\nBuilding dependencies layer', 'green');

  const layerDir = path.join(DEPENDENCIES_LAYER_DIR, 'nodejs');
  cleanDirectory(layerDir);

  // Copy package.json and package-lock.json
  log('Copying package files', 'blue');
  fs.copyFileSync(
    path.join(ROOT_DIR, 'package.json'),
    path.join(layerDir, 'package.json')
  );

  if (fs.existsSync(path.join(ROOT_DIR, 'package-lock.json'))) {
    fs.copyFileSync(
      path.join(ROOT_DIR, 'package-lock.json'),
      path.join(layerDir, 'package-lock.json')
    );
  }

  // Install production dependencies
  log('Installing production dependencies', 'blue');
  execCommand('npm ci --omit=dev --ignore-scripts', layerDir);

  // Generate Prisma Client
  log('Generating Prisma Client', 'blue');
  const schemaPath = path.join(ROOT_DIR, '..', 'prisma', 'schema.prisma');
  if (fs.existsSync(schemaPath)) {
    // Copy schema to layer directory
    fs.mkdirSync(path.join(layerDir, 'prisma'), { recursive: true });
    fs.copyFileSync(schemaPath, path.join(layerDir, 'prisma', 'schema.prisma'));

    // Generate Prisma Client in layer directory
    execCommand(`npx prisma generate --schema=${path.join(layerDir, 'prisma', 'schema.prisma')}`, layerDir);
  } else {
    log('Warning: Prisma schema not found, skipping client generation', 'yellow');
  }

  // Remove unnecessary files to reduce layer size
  log('Cleaning up unnecessary files', 'blue');
  const unnecessaryPaths = [
    'node_modules/@prisma/engines',
    'node_modules/prisma', // CLI not needed at runtime
    'node_modules/typescript', // Dev dependency
    'node_modules/effect', // Large dependency not needed at runtime
    'node_modules/fast-check', // Test dependency
    'node_modules/@types', // Type definitions not needed at runtime
    'node_modules/.bin',
    'node_modules/.cache',
    'prisma', // Remove schema after generation
  ];

  unnecessaryPaths.forEach((p) => {
    const fullPath = path.join(layerDir, p);
    if (fs.existsSync(fullPath)) {
      log(`  Removing: ${p}`, 'blue');
      fs.rmSync(fullPath, { recursive: true, force: true });
    }
  });

  // Remove darwin engine files (not needed in Lambda)
  log('Removing darwin engine binaries', 'blue');
  const prismaClientPath = path.join(layerDir, 'node_modules/.prisma/client');
  if (fs.existsSync(prismaClientPath)) {
    const files = fs.readdirSync(prismaClientPath);
    const darwinFiles = files.filter(f => f.includes('darwin'));
    darwinFiles.forEach(f => {
      const filePath = path.join(prismaClientPath, f);
      log(`  Removing: ${f}`, 'blue');
      fs.rmSync(filePath, { force: true });
    });
  }

  // Remove unnecessary Prisma WASM files for other databases (keep only PostgreSQL)
  log('Removing unnecessary database engine files', 'blue');
  const prismaRuntimePath = path.join(layerDir, 'node_modules/@prisma/client/runtime');
  if (fs.existsSync(prismaRuntimePath)) {
    const runtimeFiles = fs.readdirSync(prismaRuntimePath);
    // Keep only postgresql, remove mysql, sqlite, sqlserver, cockroachdb
    const unnecessaryEngines = runtimeFiles.filter(f => 
      (f.includes('query_engine') || f.includes('query_compiler')) &&
      (f.includes('mysql') || f.includes('sqlite') || f.includes('sqlserver') || f.includes('cockroachdb'))
    );
    
    unnecessaryEngines.forEach(f => {
      const filePath = path.join(prismaRuntimePath, f);
      log(`  Removing: ${f}`, 'blue');
      fs.rmSync(filePath, { force: true });
    });
    
    log(`  Removed ${unnecessaryEngines.length} unnecessary engine files`, 'green');
  }

  // Remove unnecessary dayjs locales (keep only en)
  log('Removing unnecessary locale files', 'blue');
  const dayjsLocalePath = path.join(layerDir, 'node_modules/dayjs/locale');
  const dayjsEsmLocalePath = path.join(layerDir, 'node_modules/dayjs/esm/locale');
  
  [dayjsLocalePath, dayjsEsmLocalePath].forEach(localePath => {
    if (fs.existsSync(localePath)) {
      const localeFiles = fs.readdirSync(localePath);
      const unnecessaryLocales = localeFiles.filter(f => 
        f.endsWith('.js') && !f.startsWith('en') && f !== 'index.js' && f !== 'index.d.ts' && f !== 'types.d.ts'
      );
      
      unnecessaryLocales.forEach(f => {
        const filePath = path.join(localePath, f);
        fs.rmSync(filePath, { force: true });
      });
      
      log(`  Removed ${unnecessaryLocales.length} locale files from ${path.basename(localePath)}`, 'green');
    }
  });

  // Remove package files (not needed in layer)
  fs.unlinkSync(path.join(layerDir, 'package.json'));
  if (fs.existsSync(path.join(layerDir, 'package-lock.json'))) {
    fs.unlinkSync(path.join(layerDir, 'package-lock.json'));
  }

  log('Dependencies layer completed', 'green');
}

function buildCommonLayer() {
  log('\nBuilding common layer', 'green');

  const layerDir = path.join(COMMON_LAYER_DIR, 'nodejs');
  cleanDirectory(layerDir);

  // Compile TypeScript common code
  log('Compiling common code', 'blue');
  execCommand('npx tsc -p common/tsconfig.json', ROOT_DIR);

  // Transform path aliases in common code using sed
  log('Transforming path aliases', 'blue');
  const commonDistDir = path.join(ROOT_DIR, 'common', 'dist');
  execCommand(
    `find ${commonDistDir} -name "*.js" -type f -exec sed -i '' 's|@/common/|/opt/nodejs/common/|g' {} +`,
    ROOT_DIR
  );

  // Copy compiled common code to layer
  if (fs.existsSync(commonDistDir)) {
    const commonLayerDir = path.join(layerDir, 'common');
    copyDirectory(commonDistDir, commonLayerDir);
    log('Common code copied to layer', 'blue');
  } else {
    log('Error: Common dist directory not found', 'red');
    throw new Error('Common compilation failed');
  }

  log('Common layer completed', 'green');
}

function buildLambdaFunctions() {
  log('\nBuilding Lambda functions', 'green');

  const lambdas = [
    'payment-webhook-handler',
    'outbox-processor',
    'outbox-cleanup',
  ];

  for (const lambda of lambdas) {
    log(`\nBuilding ${lambda}...`, 'blue');

    const lambdaDir = path.join(ROOT_DIR, lambda);
    const distDir = path.join(lambdaDir, 'dist');

    // Clean dist directory
    if (fs.existsSync(distDir)) {
      fs.rmSync(distDir, { recursive: true, force: true });
    }

    // Compile TypeScript
    execCommand(`npx tsc -p ${lambda}/tsconfig.json`, ROOT_DIR);

    // Transform path aliases (@/common/* -> /opt/nodejs/common/*) using sed
    log('Transforming path aliases', 'blue');
    execCommand(
      `find ${distDir} -name "*.js" -type f -exec sed -i '' 's|@/common/|/opt/nodejs/common/|g' {} +`,
      ROOT_DIR
    );

    log(`${lambda} completed`, 'green');
  }

  log('\nAll Lambda functions completed', 'green');
}

function createZipArchives() {
  log('\nCreating ZIP archives', 'green');

  // Check if zip command is available
  try {
    execSync('which zip', { stdio: 'ignore' });
  } catch {
    log('Warning: zip command not found, skipping archive creation', 'yellow');
    return;
  }

  // Delete old ZIP files to ensure fresh creation
  log('Removing old ZIP files', 'blue');
  const oldZips = [
    path.join(BUILD_DIR, 'dependencies-layer.zip'),
    path.join(BUILD_DIR, 'common-layer.zip'),
    path.join(BUILD_DIR, 'payment-webhook-handler.zip'),
    path.join(BUILD_DIR, 'outbox-processor.zip'),
    path.join(BUILD_DIR, 'outbox-cleanup.zip'),
  ];
  
  oldZips.forEach(zipFile => {
    if (fs.existsSync(zipFile)) {
      fs.rmSync(zipFile, { force: true });
    }
  });

  // Create dependencies layer zip
  log('Creating dependencies-layer.zip', 'blue');
  execCommand(
    `zip -r ${path.join(BUILD_DIR, 'dependencies-layer.zip')} .`,
    DEPENDENCIES_LAYER_DIR
  );

  // Create common layer zip
  log('Creating common-layer.zip', 'blue');
  execCommand(
    `zip -r ${path.join(BUILD_DIR, 'common-layer.zip')} .`,
    COMMON_LAYER_DIR
  );

  // Create Lambda function zips
  const lambdas = [
    'payment-webhook-handler',
    'outbox-processor',
    'outbox-cleanup',
  ];

  for (const lambda of lambdas) {
    log(`Creating ${lambda}.zip`, 'blue');
    const lambdaDistDir = path.join(ROOT_DIR, lambda, 'dist');

    if (fs.existsSync(lambdaDistDir)) {
      execCommand(
        `zip -r ${path.join(BUILD_DIR, `${lambda}.zip`)} .`,
        lambdaDistDir
      );
    } else {
      log(`Warning: ${lambda} dist directory not found`, 'yellow');
    }
  }

  log('ZIP archives created', 'green');
}

function printLayerSizes() {
  log('\nLayer sizes', 'green');

  const files = [
    'dependencies-layer.zip',
    'common-layer.zip',
    'payment-webhook-handler.zip',
    'outbox-processor.zip',
    'outbox-cleanup.zip',
  ];

  for (const file of files) {
    const filePath = path.join(BUILD_DIR, file);
    if (fs.existsSync(filePath)) {
      const stats = fs.statSync(filePath);
      const sizeMB = (stats.size / 1024 / 1024).toFixed(2);
      log(`${file}: ${sizeMB} MB`, 'blue');
    }
  }
}

// Main execution
async function main() {
  try {
    log('\nStarting Lambda build process', 'green');
    log(`Root: ${ROOT_DIR}`, 'blue');
    log(`Build: ${BUILD_DIR}`, 'blue');

    // Create build directory
    if (!fs.existsSync(BUILD_DIR)) {
      fs.mkdirSync(BUILD_DIR, { recursive: true });
    }

    // Build steps
    buildDependenciesLayer();
    buildCommonLayer();
    buildLambdaFunctions();
    createZipArchives();
    printLayerSizes();

    log('\nBuild completed successfully', 'green');
    log(`Output: ${BUILD_DIR}`, 'blue');
  } catch (error) {
    log('\nBuild failed', 'red');
    log(error.message, 'red');
    process.exit(1);
  }
}

// Run if called directly
if (require.main === module) {
  main();
}

module.exports = { main };

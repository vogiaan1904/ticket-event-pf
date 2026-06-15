# Plan: Hybrid Architecture Implementation & Lambda Migration

This plan details the implementation of the **Hybrid Architecture** for the Payment Service, separating the core gRPC service (EKS) from event-driven tasks (Lambda). We will structure the `ticketbottle-payment/lambdas` directory to support scalable, layer-based Lambda development as described in the [QloudX article](https://www.qloudx.com/how-to-split-typescript-based-lambda-functions-into-lambda-layers/).

## 1. Directory Structure

We will restructure `ticketbottle-payment/lambdas` to isolate functions while sharing common code.

```
ticketbottle-payment/lambdas/
├── common/                     # Shared code layer (to be deployed to /opt/nodejs)
│   ├── database.ts             # Shared Prisma Client instance
│   ├── logger.ts
│   └── constants.ts
├── payment-webhook-handler/    # Webhook Function
│   ├── index.ts
│   └── tsconfig.json           # Extends root config, maps paths to /opt
├── outbox-processor/           # Background Processor Function
│   ├── index.ts
│   └── tsconfig.json
├── outbox-cleanup/             # Cleanup Function (Daily)
│   ├── index.ts
│   └── tsconfig.json
├── package.json                # Shared dependencies for the "NodeModules" layer
└── tsconfig.json               # Root config for lambdas
```

## 2. Shared Code & Layers Strategy

Following the QloudX strategy, we will create two Lambda Layers during the build process:

1.  **Dependencies Layer**: Contains `node_modules` (Prisma, KafkaJS, etc.).
2.  **Common Code Layer**: Contains shared utilities from `lambdas/common` transpiled to JS.

### Development Workflow

-   **Local**: Use `tsconfig` paths to map `@/common/*` to `../common/*` so VS Code and `ts-node` work locally.
-   **Production**: Build scripts will move `node_modules` and transpiled `common` code into the correct layer structure (`/opt/nodejs/...`).

## 3. Implementation Steps

### Step 3.1: Setup Common Layer

-   Create `lambdas/common/database.ts` to export a singleton `PrismaClient`.
-   Create `lambdas/common/logger.ts` for consistent logging.
-   Create `lambdas/package.json` to manage shared dependencies (Prisma, KafkaJS).

### Step 3.2: Refactor Webhook Handler

-   **Source**: `lambdas/payment-callback.ts`
-   **Target**: `lambdas/payment-webhook-handler/index.ts`
-   **Action**: Move logic to the new structure, updating imports to use the local `common` module.

### Step 3.3: Implement Outbox Processor

-   **Source**: `src/modules/outbox/outbox-publisher.service.ts`
-   **Target**: `lambdas/outbox-processor/index.ts`
-   **Action**: Port the `processOutboxEvents` logic.
    -   Replace NestJS DI with direct imports from `common/database.ts`.
    -   Replace `KafkaProducerService` with a lightweight `kafkajs` producer instance.
    -   Implement the polling loop (or rely on Lambda schedule invocation).

### Step 3.4: Implement Outbox Cleanup

-   **Source**: `src/modules/outbox/outbox-publisher.service.ts` (cleanup method)
-   **Target**: `lambdas/outbox-cleanup/index.ts`
-   **Action**: Port the `cleanupOldEvents` logic.

## 4. Build & Deployment Plan

We will add scripts to `ticketbottle-payment/package.json` to handle the build:

1.  **`build:lambdas`**:

    -   Transpile all functions and common code using `tsc`.
    -   Organize output into `dist/layers/common`, `dist/layers/node_modules`, and `dist/functions/*`.

2.  **Layer Structure**:

    -   `node_modules` -> `dist/layers/node_modules/nodejs/node_modules`
    -   `common` -> `dist/layers/common/nodejs/common`

## 5. Next Steps

1.  Create the directory structure.
2.  Initialize `lambdas/package.json` and install dependencies.
3.  Migrate the code.
FROM node:20-slim AS build
WORKDIR /app/outbox-relay
COPY outbox-relay/package*.json ./
RUN npm install
COPY lambdas/common /app/lambdas/common
RUN ln -s /app/outbox-relay/node_modules /app/node_modules
COPY outbox-relay/tsconfig.json ./
COPY outbox-relay/src ./src
RUN npm run build

FROM node:20-slim
WORKDIR /app/outbox-relay
COPY --from=build /app/outbox-relay/node_modules ./node_modules
COPY --from=build /app/outbox-relay/dist ./dist
RUN ln -s /app/outbox-relay/node_modules /app/node_modules
USER node
CMD ["node", "dist/outbox-relay/src/index.js"]

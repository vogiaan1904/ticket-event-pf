# Build stage
FROM node:20-alpine AS builder

# Set working directory
WORKDIR /app

# Copy package files and install dependencies
COPY package*.json ./
RUN npm ci

# Copy Prisma configuration and schema
COPY prisma.config.ts ./
COPY prisma ./prisma/

# Generate Prisma client
RUN npx prisma generate

# Copy the rest of the application
COPY . .

# Build the application
RUN npm run build

# Production stage
FROM node:20-alpine AS production

# Set environment variables
ENV NODE_ENV=production
ENV USER=eventapp
ENV GROUP=eventapp
ENV UID=2023
ENV GID=2023

# Set working directory
WORKDIR /app

# Create non-root user
RUN addgroup -g $GID -S $GROUP \
    && adduser -D -S -h /app -s /sbin/nologin -G $GROUP -u $UID $USER

# Copy package files
COPY package.json package-lock.json ./

# Install production dependencies only
RUN npm ci --omit=dev --prefer-offline --no-audit && \
    npm cache clean --force

# Copy Prisma schema and generated client from builder
COPY --from=builder /app/node_modules/.prisma ./node_modules/.prisma
COPY --from=builder /app/prisma ./prisma

# Copy built application
COPY --from=builder /app/dist ./dist

# Switch to non-root user
USER $USER

# Expose the application port
EXPOSE 80

# Start the application
CMD ["node", "dist/src/main"]
# Build stage
FROM node:20-alpine AS builder

# Set working directory
WORKDIR /app

# Copy package files and install dependencies
COPY package*.json ./
RUN npm ci

# Copy the rest of the application
COPY . .

# Build the application
RUN npm run build

# Production stage
FROM node:20-alpine AS production

# Set environment variables
ENV NODE_ENV=production
ENV USER=apigatewayapp
ENV GROUP=apigatewayap
ENV UID=2021
ENV GID=2021

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

# Copy built application
COPY --from=builder /app/dist ./dist

# Switch to non-root user
USER $USER

# Expose the application port
EXPOSE 80

# Start the application
CMD ["node", "dist/main"]
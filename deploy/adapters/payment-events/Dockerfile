FROM node:20-alpine
WORKDIR /app
COPY package.json ./
RUN npm install --omit=dev --no-audit --no-fund
COPY webhook.js ./
# default entrypoint; overridden per workload via the pod command
CMD ["node", "webhook.js"]

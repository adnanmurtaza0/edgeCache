FROM node:alpine
WORKDIR /app
COPY package.json tsconfig.json ./
RUN npm install --no-audit --no-fund
COPY src ./src
EXPOSE 3000
CMD ["npm", "start"]
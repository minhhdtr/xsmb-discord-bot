# Two stages: build with the dev dependencies, ship without them.
#
# The dependency tree has no native binaries, which is what makes a lockfile
# written on one platform safe to install on another. That is not luck — it is
# why the tests run on Node's built-in runner rather than a framework that
# drags in rollup and esbuild. Adding a dependency that ships a .node file
# would quietly reintroduce the problem, on the machine furthest from anyone
# who could debug it.
FROM node:22-alpine AS build
WORKDIR /app

COPY package.json package-lock.json* ./
RUN npm install --no-audit --no-fund

COPY tsconfig.json ./
COPY src ./src
# tsconfig also lists test/, which is absent here. An include pattern that
# matches nothing is not an error as long as another one matches, and rootDir
# is pinned to "." so the output lands at dist/src/ either way.
RUN npx tsc

# Drop the dev dependencies now the build is done, rather than resolving the
# tree a second time.
RUN npm prune --omit=dev

FROM node:22-alpine
WORKDIR /app
ENV NODE_ENV=production

COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
COPY package.json ./

USER node
CMD ["node", "dist/src/index.js"]

# Frontend Development

This project uses [Vite](https://vitejs.dev/) as the build tool.

## Local Development

Before starting the frontend, ensure the backend services are running:

1. Start the backend services (from the project root):

   ```bash
   make local-dashboard-dev DSN="postgres://user:pass@localhost:5432/ship_status?sslmode=disable"
   ```

2. Install frontend dependencies (if not already done):

   ```bash
   npm install
   ```

3. Start the development server with required environment variables:

   ```bash
   VITE_PUBLIC_DOMAIN=http://localhost:8180 \
   VITE_PROTECTED_DOMAIN=http://localhost:3030 \
   npm start
   ```

   Optional: set `VITE_DASHBOARD_REFRESH_INTERVAL_MS` (milliseconds) to change how often
   pages poll for live data. Default is `300000` (5 minutes). For faster local
   testing, put `VITE_DASHBOARD_REFRESH_INTERVAL_MS=15000` in `.env.development.local`
   (gitignored), not in the committed `.env.development`.

The app will open at [http://localhost:3030](http://localhost:3030).

**Note:** The backend provides two routes:

- Public route (port 8180): No authentication required
- Protected route (port 8443): Requires basic auth (`developer:password`)

In local dev, Login stays on port 3030. Vite proxies `/oauth` and protected `/api` calls to port 8443, so the browser does not open that port itself. Sign in with `developer:password` when the browser asks.

## Available Scripts

In the project directory, you can run:

### `npm start`

Runs the app in development mode using Vite.\
Open [http://localhost:3030](http://localhost:3030) to view it in the browser.

The page will reload if you make edits.\
You will also see any lint errors in the console.

### `npm run build`

Builds the app for production to the `build` folder.\
The build is optimized and minified for the best performance.

### `npm run preview`

Preview the production build locally.

### `npm test`

Tests need to be configured with Vitest or Jest.

### `npm run lint`

Runs ESLint to check for code quality issues.

## Learn More

To learn React, check out the [React documentation](https://reactjs.org/).

To learn Vite, check out the [Vite documentation](https://vitejs.dev/).

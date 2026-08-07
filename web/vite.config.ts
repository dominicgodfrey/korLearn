import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// The Go binary serves web/dist in production and knows nothing about Vite.
// In development the two run side by side, so /api is proxied to the binary
// rather than duplicating its routes or enabling CORS.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
});

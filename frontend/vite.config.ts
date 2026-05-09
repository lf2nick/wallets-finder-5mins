import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// dev mode 把 /api proxy 到本機 backend (:8080)，免 CORS / 同 origin 開發
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
  build: {
    outDir: "dist",
    sourcemap: false,
  },
});

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react-swc";
import tailwindcss from "@tailwindcss/vite";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/v1": {
        target: process.env.VITE_AUTH_API_BASE_URL || "http://localhost:10009",
        changeOrigin: true,
      },
    },
  },
});

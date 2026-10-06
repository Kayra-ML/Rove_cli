import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "url";
import { resolve, dirname } from "path";

const __dirname = dirname(fileURLToPath(import.meta.url));

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "~": resolve(__dirname, "src") },
  },
  server: { port: 5173, strictPort: true },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // libraries change far less often than the app: their own files
        manualChunks: {
          react: ["react", "react-dom", "react-dom/client"],
          markdown: ["react-markdown", "remark-gfm", "remark-breaks"],
        },
      },
    },
  },
  test: { environment: "jsdom", setupFiles: ["src/test-setup.ts"] },
});

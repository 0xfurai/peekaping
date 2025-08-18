import path from "path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react-swc";
import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");

  return {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "./src"),
      },
    },
    server: {
      proxy: {
        "/socket.io": {
          target: env.API_URL ?? "http://localhost:8034",
          ws: true,
          changeOrigin: true,
        },
        "/api": {
          target: env.API_URL ?? "http://localhost:8034",
          changeOrigin: true,
          configure: (proxy, options) => {
            proxy.on("proxyReq", (_, req) => {
              console.log(
                `[PROXY] ${req.method} ${req.url} -> ${options.target}`
              );
            });

            proxy.on("proxyRes", (proxyRes, req) => {
              console.log(
                `[PROXY RESPONSE] ${req.method} ${req.url} <- ${options.target} (${proxyRes.statusCode})`
              );
            });

            proxy.on("error", (err, req) => {
              console.error(
                `[PROXY ERROR] ${req.method} ${req.url}:`,
                err.message
              );
            });
          },
        },
      },
    },
  };
});

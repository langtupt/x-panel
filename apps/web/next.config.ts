import type { NextConfig } from "next";

// Production: static export served by xpanel-api (no Node.js on the server).
// Development: proxy /api to a local xpanel-api on :8888.
const isProd = process.env.NODE_ENV === "production";

const config: NextConfig = isProd
  ? { output: "export", trailingSlash: true, images: { unoptimized: true } }
  : {
      trailingSlash: true,
      skipTrailingSlashRedirect: true,
      async rewrites() {
        return [{ source: "/api/:path*", destination: "http://127.0.0.1:8888/api/:path*" }];
      },
    };

export default config;

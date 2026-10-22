const apiBase = (process.env.API_INTERNAL_URL || "http://localhost:8080").replace(/\/+$/, "");

const nextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
  output: "standalone",
  experimental: {
    proxyTimeout: 150_000,
  },
  async rewrites() {
    return [
      {
        source: "/healthz",
        destination: `${apiBase}/healthz`,
      },
      {
        source: "/readyz",
        destination: `${apiBase}/readyz`,
      },
      {
        source: "/api/:path*",
        destination: `${apiBase}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;

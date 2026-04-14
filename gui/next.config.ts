import type { NextConfig } from "next";

const config: NextConfig = {
  // Server components fetch the MockAgents management API directly;
  // disable static optimization so each page is always rendered against
  // the latest server state.
  experimental: {},
  reactStrictMode: true,
};

export default config;

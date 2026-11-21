import type { NextConfig } from "next";

const nextConfig: NextConfig = {
	output: "export",
	env: { NEXT_PUBLIC_BOARD_BUILD: process.env.GITHUB_SHA?.slice(0, 12) ?? "" },
};

export default nextConfig;

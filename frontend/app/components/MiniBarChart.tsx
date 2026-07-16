"use client";

import { useState } from "react";
import type { ChartDataPoint } from "../lib/types";
import { formatCompact } from "../lib/utils";

interface MiniBarChartProps {
  data: ChartDataPoint[];
  height?: number;
  barColor?: string;
  emptyMessage?: string;
}

export function MiniBarChart({
  data,
  height = 180,
  barColor = "#3b82f6",
  emptyMessage = "No data for this period",
}: MiniBarChartProps) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null);

  if (data.length === 0) {
    return (
      <div
        className="flex items-center justify-center text-[13px] text-zinc-600"
        style={{ height }}
      >
        {emptyMessage}
      </div>
    );
  }

  const maxValue = Math.max(...data.map((d) => d.value), 1);

  return (
    <div className="w-full">
      {/* Tooltip */}
      {hoveredIndex !== null && (
        <div className="mb-2 text-right text-[12px] text-zinc-400 transition-opacity">
          <span className="font-medium text-zinc-200">
            {formatCompact(data[hoveredIndex].value)}
          </span>{" "}
          — {data[hoveredIndex].label}
        </div>
      )}

      {/* Bars */}
      <div
        className="flex items-end gap-[3px]"
        style={{ height }}
        onMouseLeave={() => setHoveredIndex(null)}
      >
        {data.map((d, i) => {
          const pct = (d.value / maxValue) * 100;
          const isHovered = hoveredIndex === i;

          return (
            <div
              key={i}
              className="group flex flex-1 cursor-pointer flex-col items-center"
              style={{ height: "100%" }}
              onMouseEnter={() => setHoveredIndex(i)}
            >
              <div className="flex flex-1 w-full items-end">
                <div
                  className="w-full rounded-t transition-all duration-300"
                  style={{
                    height: `${Math.max(pct, 2)}%`,
                    backgroundColor: barColor,
                    opacity: isHovered ? 1 : 0.7,
                  }}
                />
              </div>
            </div>
          );
        })}
      </div>

      {/* Labels */}
      <div className="mt-1.5 flex gap-[3px]">
        {data.map((d, i) => (
          <div key={i} className="flex-1 text-center">
            <span className="text-[9px] leading-none text-zinc-600 truncate block">
              {/* Show every Nth label to avoid crowding */}
              {data.length <= 14 || i % Math.ceil(data.length / 14) === 0
                ? d.label
                : ""}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

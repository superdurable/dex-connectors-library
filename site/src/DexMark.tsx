export function DexMark({ size = 30 }: { size?: number }) {
  const center = size / 2;
  const radius = size * 0.336;
  const strokeWidth = size * 0.15;
  const pointAt = (degrees: number): [number, number] => {
    const radians = (degrees * Math.PI) / 180;
    return [center + radius * Math.cos(radians), center + radius * Math.sin(radians)];
  };
  const [headX, headY] = pointAt(-62);
  const [startX, startY] = pointAt(-88);
  const [endX, endY] = pointAt(-362);
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      height={size}
      viewBox={`0 0 ${size} ${size}`}
      width={size}
    >
      <path
        d={`M ${endX.toFixed(2)} ${endY.toFixed(2)} A ${radius.toFixed(2)} ${radius.toFixed(2)} 0 1 1 ${startX.toFixed(2)} ${startY.toFixed(2)}`}
        fill="none"
        stroke="var(--brand-track)"
        strokeLinecap="round"
        strokeWidth={strokeWidth.toFixed(2)}
      />
      <circle cx={headX.toFixed(2)} cy={headY.toFixed(2)} fill="var(--brand-head)" r={(strokeWidth * 0.8).toFixed(2)} />
    </svg>
  );
}

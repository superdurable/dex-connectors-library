type DexMarkProps = {
  size?: number;
};

export function DexMark({ size = 36 }: DexMarkProps) {
  const center = size / 2;
  const radius = size * 0.336;
  const stroke = size * 0.15;
  return (
    <svg className="dex-mark" width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden="true">
      <circle cx={center} cy={center} r={radius} fill="none" stroke="var(--brand-track)" strokeWidth={stroke} />
      <path
        d={arc(center, radius, -88, -62)}
        fill="none"
        stroke="var(--brand-head)"
        strokeWidth={stroke}
        strokeLinecap="round"
      />
      <path
        d={arc(center, radius, -88, -362)}
        fill="none"
        stroke="var(--brand-track)"
        strokeWidth={stroke}
      />
    </svg>
  );
}

function arc(center: number, radius: number, startAngle: number, endAngle: number) {
  const start = point(center, radius, startAngle);
  const end = point(center, radius, endAngle);
  const large = endAngle - startAngle <= 180 ? 0 : 1;
  return `M ${start.x} ${start.y} A ${radius} ${radius} 0 ${large} 1 ${end.x} ${end.y}`;
}

function point(center: number, radius: number, angle: number) {
  const radians = (angle * Math.PI) / 180;
  return { x: center + radius * Math.cos(radians), y: center + radius * Math.sin(radians) };
}

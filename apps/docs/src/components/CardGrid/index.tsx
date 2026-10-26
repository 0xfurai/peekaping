import type { MouseEvent, ReactNode } from "react";
import Link from "@docusaurus/Link";
import clsx from "clsx";
import styles from "./styles.module.css";

// Lucide-style stroke icons, drawn on a 24px grid
const ICONS: Record<string, ReactNode> = {
  rocket: (
    <>
      <path d="M4.5 16.5c-1.5 1.26-2 5-2 5s3.74-.5 5-2c.71-.84.7-2.13-.09-2.91a2.18 2.18 0 0 0-2.91-.09z" />
      <path d="m12 15-3-3a22 22 0 0 1 2-3.95A12.88 12.88 0 0 1 22 2c0 2.72-.78 7.5-6 11a22.35 22.35 0 0 1-4 2z" />
      <path d="M9 12H4s.55-3.03 2-4c1.62-1.08 5 0 5 0M12 15v5s3.03-.55 4-2c1.08-1.62 0-5 0-5" />
    </>
  ),
  layers: (
    <>
      <path d="m12 2 10 5-10 5L2 7z" />
      <path d="m2 17 10 5 10-5M2 12l10 5 10-5" />
    </>
  ),
  ship: (
    <>
      <circle cx="12" cy="12" r="9" />
      <circle cx="12" cy="12" r="2.5" />
      <path d="M12 3v6.5M12 14.5V21M3 12h6.5M14.5 12H21" />
    </>
  ),
  radar: (
    <>
      <path d="M19.07 4.93A10 10 0 0 0 6.99 3.34M4 6h.01M2.29 9.62a10 10 0 1 0 19.02-1.27" />
      <path d="M16.24 7.76a6 6 0 1 0-8.01 8.91M12 18h.01M17.99 11.66a6 6 0 0 1-2.22 5.01" />
      <circle cx="12" cy="12" r="2" />
      <path d="m13.41 10.59 5.66-5.66" />
    </>
  ),
  bell: (
    <>
      <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
      <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
    </>
  ),
  cpu: (
    <>
      <rect x="4" y="4" width="16" height="16" rx="2" />
      <rect x="9" y="9" width="6" height="6" />
      <path d="M9 1v3M15 1v3M9 20v3M15 20v3M20 9h3M20 14h3M1 9h3M1 14h3" />
    </>
  ),
  badge: (
    <>
      <rect x="2" y="7" width="20" height="10" rx="5" />
      <path d="M12 7v10" />
      <circle cx="7" cy="12" r="1.5" />
    </>
  ),
  code: (
    <>
      <path d="m16 18 6-6-6-6M8 6l-6 6 6 6" />
    </>
  ),
  activity: <path d="M22 12h-4l-3 9L9 3l-3 9H2" />,
  chart: (
    <>
      <path d="M3 3v18h18" />
      <path d="m19 9-5 5-4-4-3 3" />
    </>
  ),
  page: (
    <>
      <path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5z" />
      <path d="M14 2v6h6M8 13h8M8 17h5" />
    </>
  ),
  shield: (
    <>
      <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10" />
      <path d="m9 12 2 2 4-4" />
    </>
  ),
};

export function Card({
  icon,
  title,
  to,
  children,
}: {
  icon: keyof typeof ICONS;
  title: string;
  to?: string;
  children: ReactNode;
}): ReactNode {
  // Moves the hover glow under the pointer
  const track = (e: MouseEvent<HTMLElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    e.currentTarget.style.setProperty("--mx", `${e.clientX - rect.left}px`);
    e.currentTarget.style.setProperty("--my", `${e.clientY - rect.top}px`);
  };

  const body = (
    <>
      <span className={styles.icon} aria-hidden="true">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round">
          {ICONS[icon]}
        </svg>
      </span>
      <span className={styles.title}>
        {title}
        {to && (
          <span className={styles.arrow} aria-hidden="true">
            →
          </span>
        )}
      </span>
      <div className={styles.text}>{children}</div>
    </>
  );

  return to ? (
    <Link to={to} className={clsx(styles.card, styles.linked, "card-link")} onMouseMove={track}>
      {body}
    </Link>
  ) : (
    <div className={styles.card} onMouseMove={track}>
      {body}
    </div>
  );
}

export default function CardGrid({ children, cols = 3 }: { children: ReactNode; cols?: 2 | 3 }): ReactNode {
  return <div className={clsx(styles.grid, cols === 2 && styles.cols2)}>{children}</div>;
}

import { useState, type ReactNode } from "react";
import Link from "@docusaurus/Link";
import useBaseUrl from "@docusaurus/useBaseUrl";
import styles from "./styles.module.css";

const QUICKSTART = "docker run -d -p 8383:8383 -v peekaping:/app/data 0xfurai/peekaping-bundle-sqlite";

function CopyCommand({ command }: { command: string }): ReactNode {
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      // Clipboard can be blocked; the command stays selectable
    }
  };

  return (
    <button type="button" className={styles.command} onClick={copy} aria-label="Copy the docker run command">
      <span className={styles.prompt} aria-hidden="true">
        $
      </span>
      <code>{command}</code>
      <span className={styles.copyHint}>{copied ? "Copied" : "Copy"}</span>
    </button>
  );
}

export default function DocHero(): ReactNode {
  return (
    <header className={styles.hero}>
      <a className={styles.badge} href="https://github.com/0xfurai/peekaping/releases" target="_blank" rel="noopener noreferrer">
        <span className={styles.pulse} aria-hidden="true" />
        Open source · MIT licensed · Self-hosted
        <span aria-hidden="true">→</span>
      </a>

      <h1 className={styles.title}>
        Peekaping <span className={styles.gradient}>docs</span>
      </h1>

      <p className={styles.lead}>
        Uptime monitoring you run on your own server. Check websites, APIs, databases and queues, get alerts where your team
        already talks, and publish status pages.
      </p>

      <div className={styles.actions}>
        <Link className={styles.btnPrimary} to="/self-hosting/docker-with-sqlite">
          Get started <span aria-hidden="true">→</span>
        </Link>
        <Link className={styles.btnGhost} href="https://demo.peekaping.com">
          Try the live demo
        </Link>
      </div>

      <CopyCommand command={QUICKSTART} />

      <div className={styles.shot}>
        <img
          src={useBaseUrl("/img/demo.webp")}
          alt="Peekaping monitor page: current status, a heartbeat bar, uptime for 24 hours to 365 days and a response time chart"
          width={2236}
          height={1438}
        />
      </div>
    </header>
  );
}

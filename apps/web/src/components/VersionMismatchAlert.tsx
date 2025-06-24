import { useQuery } from "@tanstack/react-query";
import { getVersionOptions } from "../api/@tanstack/react-query.gen";
import { VERSION } from "../version";
import { useState } from "react";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

function getStorageKey(clientVersion: string, serverVersion: string) {
  return `version-mismatch-dismissed:${clientVersion}:${serverVersion}`;
}

export function VersionMismatchAlert() {
  const { data: serverVersionData, isLoading } = useQuery(getVersionOptions());
  const serverVersion = serverVersionData?.version ?? null;
  const versionMismatch = serverVersion && serverVersion !== VERSION;
  const { t } = useLocalizedTranslation();

  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const [_, rerender] = useState(false);

  if (isLoading || !versionMismatch) return null;

  const handleClose = () => {
    localStorage.setItem(getStorageKey(VERSION, serverVersion), "true");
    rerender(true);
  };

  const dismissed =
    localStorage.getItem(getStorageKey(VERSION, serverVersion)) === "true";
  if (dismissed) return null;

  return (
    <div
      style={{ zIndex: 9999 }}
      className="fixed top-0 left-0 w-full bg-red-600 text-white text-center py-2 font-semibold shadow-lg flex items-center justify-center"
    >
      <span>
        {t("messages.version_mismatch")}: client v{VERSION}, server v
        {serverVersion}. Please refresh the page.
      </span>
      <button
        onClick={handleClose}
        className="ml-4 px-2 py-0.5 rounded bg-red-800 hover:bg-red-700 text-white text-xs font-bold"
        style={{ marginLeft: 16 }}
      >
        {t("common.dismiss")}
      </button>
    </div>
  );
}

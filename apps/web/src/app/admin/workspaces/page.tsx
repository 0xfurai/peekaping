import { useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { getAdminWorkspacesOptions } from "@/api/@tanstack/react-query.gen";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useDebounce } from "@/hooks/useDebounce";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import Layout from "@/layout";
import { WorkspacesTable } from "./workspaces-table";

const PAGE_SIZE = 20;
const SEARCH_DELAY_MS = 300;

// The workspaces of the install (/admin/workspaces): each with its owners, members and counts of
// what it holds, and what an instance admin can do to it. Nothing inside a workspace shows here
// (docs/adr/0005, section 10). Search finds a name or an owner's email, and the list pages.
const WorkspacesPage = () => {
  const { t } = useLocalizedTranslation();
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(0);
  const q = useDebounce(search.trim(), SEARCH_DELAY_MS);

  const { data, isLoading, isError } = useQuery({
    ...getAdminWorkspacesOptions({ query: { ...(q ? { q } : {}), page, limit: PAGE_SIZE } }),
    // Typing or paging keeps the rows on screen until the next ones arrive
    placeholderData: keepPreviousData,
  });

  const workspaces = data?.data.workspaces ?? [];
  const total = data?.data.total ?? 0;
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <Layout pageName={t("admin.workspaces.page_name")} isLoading={isLoading}>
      <div className="flex flex-col gap-4">
        <p className="text-sm text-muted-foreground">{t("admin.workspaces.description")}</p>

        <div className="flex flex-wrap items-center gap-3">
          <Input
            type="search"
            className="max-w-sm"
            aria-label={t("admin.workspaces.search_label")}
            placeholder={t("admin.workspaces.search_placeholder")}
            value={search}
            onChange={(event) => {
              setSearch(event.target.value);
              setPage(0);
            }}
          />
          {data && (
            <span className="text-sm text-muted-foreground">
              {t("admin.workspaces.total", { total })}
            </span>
          )}
        </div>

        {isError && <p className="text-sm text-destructive">{t("admin.workspaces.load_failed")}</p>}

        {data && workspaces.length === 0 && (
          <p className="text-sm text-muted-foreground">{t("admin.workspaces.empty")}</p>
        )}

        {workspaces.length > 0 && (
          <Card>
            <CardContent>
              <WorkspacesTable workspaces={workspaces} />
            </CardContent>
          </Card>
        )}

        {pages > 1 && (
          <div className="flex items-center justify-end gap-3">
            <span className="text-sm text-muted-foreground">
              {t("admin.workspaces.paging.page", { page: page + 1, pages })}
            </span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={page === 0}
              onClick={() => setPage(page - 1)}
            >
              {t("admin.workspaces.paging.previous")}
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={page + 1 >= pages}
              onClick={() => setPage(page + 1)}
            >
              {t("admin.workspaces.paging.next")}
            </Button>
          </div>
        )}
      </div>
    </Layout>
  );
};

export default WorkspacesPage;

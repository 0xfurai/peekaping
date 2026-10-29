import { useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { getAdminUsersOptions } from "@/api/@tanstack/react-query.gen";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useDebounce } from "@/hooks/useDebounce";
import { useMe } from "@/hooks/useMe";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import Layout from "@/layout";
import { UsersTable } from "./users-table";

const PAGE_SIZE = 20;
const SEARCH_DELAY_MS = 300;

// The users of the install (/admin/users): everyone with an account, the workspaces they are in,
// and what an instance admin can do to them. Search finds an email, and the list pages.
const UsersPage = () => {
  const { t } = useLocalizedTranslation();
  const currentUserId = useMe().data?.data?.user.id;
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(0);
  const q = useDebounce(search.trim(), SEARCH_DELAY_MS);

  const { data, isLoading, isError } = useQuery({
    ...getAdminUsersOptions({ query: { ...(q ? { q } : {}), page, limit: PAGE_SIZE } }),
    // Typing or paging keeps the rows on screen until the next ones arrive
    placeholderData: keepPreviousData,
  });

  const users = data?.data.users ?? [];
  const total = data?.data.total ?? 0;
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <Layout pageName={t("admin.users.page_name")} isLoading={isLoading}>
      <div className="flex flex-col gap-4">
        <p className="text-sm text-muted-foreground">{t("admin.users.description")}</p>

        <div className="flex flex-wrap items-center gap-3">
          <Input
            type="search"
            className="max-w-sm"
            aria-label={t("admin.users.search_label")}
            placeholder={t("admin.users.search_placeholder")}
            value={search}
            onChange={(event) => {
              setSearch(event.target.value);
              setPage(0);
            }}
          />
          {data && (
            <span className="text-sm text-muted-foreground">
              {t("admin.users.total", { total })}
            </span>
          )}
        </div>

        {isError && <p className="text-sm text-destructive">{t("admin.users.load_failed")}</p>}

        {data && users.length === 0 && (
          <p className="text-sm text-muted-foreground">{t("admin.users.empty")}</p>
        )}

        {users.length > 0 && (
          <Card>
            <CardContent>
              <UsersTable
                users={users}
                currentUserId={currentUserId}
                complete={q === "" && total === users.length}
              />
            </CardContent>
          </Card>
        )}

        {pages > 1 && (
          <div className="flex items-center justify-end gap-3">
            <span className="text-sm text-muted-foreground">
              {t("admin.users.paging.page", { page: page + 1, pages })}
            </span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={page === 0}
              onClick={() => setPage(page - 1)}
            >
              {t("admin.users.paging.previous")}
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={page + 1 >= pages}
              onClick={() => setPage(page + 1)}
            >
              {t("admin.users.paging.next")}
            </Button>
          </div>
        )}
      </div>
    </Layout>
  );
};

export default UsersPage;

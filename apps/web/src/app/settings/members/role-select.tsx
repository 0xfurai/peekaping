import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import type { MemberRole } from "@/lib/member-roles";

// A native select of roles, styled like an Input. The caller passes only the roles it may offer.
export function RoleSelect({
  roles,
  className,
  ...props
}: { roles: MemberRole[] } & Omit<ComponentProps<"select">, "children">) {
  const { t } = useLocalizedTranslation();
  return (
    <select
      data-slot="select"
      className={cn(
        "border-input dark:bg-input/30 h-9 rounded-md border bg-transparent px-2 text-sm shadow-xs outline-none disabled:cursor-not-allowed disabled:opacity-50",
        "focus-visible:border-ring focus-visible:ring-ring/50 focus-visible:ring-[3px]",
        className
      )}
      {...props}
    >
      {roles.map((role) => (
        <option key={role} value={role} className="bg-popover text-popover-foreground">
          {t(`members.roles.${role}`)}
        </option>
      ))}
    </select>
  );
}

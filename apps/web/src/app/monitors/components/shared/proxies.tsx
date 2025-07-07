import { getProxiesOptions } from "@/api/@tanstack/react-query.gen";
import { Button } from "@/components/ui/button";
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { TypographyH4 } from "@/components/ui/typography";
import { useQuery } from "@tanstack/react-query";
import { useFormContext } from "react-hook-form";
import { z } from "zod";

export const proxiesSchema = z.object({
  proxy_id: z.string().optional(),
});

export const proxiesDefaultValues = {
  proxy_id: undefined,
};

const Proxies = ({ onNewProxy }: { onNewProxy: () => void }) => {
  const form = useFormContext();
  const proxy_id = form.watch("proxies.proxy_id");

  const { data: proxies } = useQuery({
    ...getProxiesOptions(),
  });

  return (
    <div className="flex flex-col gap-2">
      <TypographyH4 className="mb-2">Proxy</TypographyH4>

      {proxy_id && (
        <>
          <Label>Selected Proxy</Label>
          <div className="flex flex-col gap-1 mb-2">
            {(() => {
              const proxy = proxies?.data?.find((p) => p.id === proxy_id);
              if (!proxy) return null;
              return (
                <div
                  key={proxy_id}
                  className="flex items-center justify-between bg-muted rounded px-3 py-1"
                >
                  <span>{`${proxy.protocol}://${proxy.host}:${proxy.port}`}</span>
                  <Button
                    type="button"
                    size="icon"
                    variant="ghost"
                    onClick={() => {
                      form.setValue("proxies.proxy_id", "", {
                        shouldDirty: true,
                      });
                    }}
                    aria-label={`Remove proxy ${proxy.host}`}
                  >
                    ×
                  </Button>
                </div>
              );
            })()}
          </div>
        </>
      )}

      <div className="flex items-center gap-2">
        <FormField
          control={form.control}
          name="proxy_id"
          render={({ field }) => {
            const availableProxies = proxies?.data || [];
            return (
              <FormItem className="flex-1">
                <FormLabel>Add Proxy</FormLabel>
                <FormControl>
                  <Select
                    value={field.value || "none"}
                    onValueChange={(val) => {
                      if (val === "none") {
                        field.onChange("", { shouldDirty: true });
                      } else if (val) {
                        field.onChange(val, { shouldDirty: true });
                      }
                    }}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue placeholder="Select Proxy" />
                    </SelectTrigger>

                    <SelectContent>
                      <SelectItem value="none">
                        {proxy_id ? "Remove Proxy" : "No Proxy"}
                      </SelectItem>
                      {availableProxies.map((p) => (
                        <SelectItem key={p.id} value={p.id || "none"}>
                          {`${p.protocol}://${p.host}:${p.port}`}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </FormControl>
                <FormMessage />
              </FormItem>
            );
          }}
        />
        <Button
          type="button"
          onClick={onNewProxy}
          variant="outline"
          className="self-end"
        >
          + New Proxy
        </Button>
      </div>
    </div>
  );
};

export default Proxies;

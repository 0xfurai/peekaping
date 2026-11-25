import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { TypographyH4 } from "@/components/ui/typography";
import { useLocalizedTranslation } from "@/hooks/useTranslation";
import { useFormContext } from "react-hook-form";
import type { SlowResponseMode } from "./schema";

const SlowResponse = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();
  const mode: SlowResponseMode = form.watch("slow_response_mode") ?? "off";

  return (
    <div className="space-y-4">
      <TypographyH4>
        {t("monitors.form.shared.slow_response.title")}
      </TypographyH4>

      <FormField
        control={form.control}
        name="slow_response_mode"
        render={({ field }) => (
          <FormItem>
            <FormLabel>
              {t("monitors.form.shared.slow_response.mode_label")}
            </FormLabel>
            <Select
              value={field.value ?? "off"}
              onValueChange={(value) => field.onChange(value)}
            >
              <FormControl>
                <SelectTrigger className="w-full" data-testid="slow-response-mode-select">
                  <SelectValue />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                <SelectItem value="off">
                  {t("monitors.form.shared.slow_response.mode_off")}
                </SelectItem>
                <SelectItem value="fixed">
                  {t("monitors.form.shared.slow_response.mode_fixed")}
                </SelectItem>
                <SelectItem value="baseline">
                  {t("monitors.form.shared.slow_response.mode_baseline")}
                </SelectItem>
              </SelectContent>
            </Select>
            <FormDescription>
              {t("monitors.form.shared.slow_response.description")}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />

      {mode === "fixed" && (
        <FormField
          control={form.control}
          name="slow_response_threshold_ms"
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                {t("monitors.form.shared.slow_response.threshold_label")}
              </FormLabel>
              <FormControl>
                <Input
                  type="number"
                  min={1}
                  placeholder="500"
                  name={field.name}
                  ref={field.ref}
                  onBlur={field.onBlur}
                  value={field.value ?? ""}
                  onChange={(e) => field.onChange(e.target.value)}
                />
              </FormControl>
              <FormDescription>
                {t("monitors.form.shared.slow_response.threshold_description")}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}

      {mode === "baseline" && (
        <>
          <FormField
            control={form.control}
            name="slow_response_multiplier"
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t("monitors.form.shared.slow_response.multiplier_label")}
                </FormLabel>
                <FormControl>
                  <Input
                    type="number"
                    min={1}
                    step="any"
                    placeholder="2"
                    name={field.name}
                    ref={field.ref}
                    onBlur={field.onBlur}
                    value={field.value ?? ""}
                    onChange={(e) => field.onChange(e.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t("monitors.form.shared.slow_response.multiplier_description")}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name="slow_response_min_ms"
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t("monitors.form.shared.slow_response.min_ms_label")}
                </FormLabel>
                <FormControl>
                  <Input
                    type="number"
                    min={0}
                    placeholder="100"
                    name={field.name}
                    ref={field.ref}
                    onBlur={field.onBlur}
                    value={field.value ?? ""}
                    onChange={(e) => field.onChange(e.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t("monitors.form.shared.slow_response.min_ms_description")}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </>
      )}

      {mode !== "off" && (
        <FormField
          control={form.control}
          name="slow_response_consecutive"
          render={({ field }) => (
            <FormItem>
              <FormLabel>
                {t("monitors.form.shared.slow_response.consecutive_label")}
              </FormLabel>
              <FormControl>
                <Input
                  type="number"
                  min={1}
                  placeholder="3"
                  {...field}
                />
              </FormControl>
              <FormDescription>
                {t("monitors.form.shared.slow_response.consecutive_description")}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      )}
    </div>
  );
};

export default SlowResponse;

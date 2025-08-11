import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { getTimezoneOffsetLabel, sortedTimezones } from "@/lib/timezones";
import { useFormContext } from "react-hook-form";
import { useLocalizedTranslation } from "@/hooks/useTranslation";

const Timezone = () => {
  const { t } = useLocalizedTranslation();
  const form = useFormContext();
  
  const timezoneOptions = [
    { value: "SAME_AS_SERVER", label: t("maintenance.form.same_as_server") },
    { value: "UTC", label: "UTC" },
    ...sortedTimezones.map((el) => ({
      value: el,
      label: `${el} (${getTimezoneOffsetLabel(el)})`,
    })),
  ];

  return (
    <FormField
      control={form.control}
      name="timezone"
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t("maintenance.form.timezone_label")}</FormLabel>
          <Select onValueChange={field.onChange} value={field.value}>
            <FormControl>
              <SelectTrigger>
                <SelectValue placeholder={t("maintenance.form.timezone_placeholder")} />
              </SelectTrigger>
            </FormControl>
            <SelectContent>
              {timezoneOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <FormMessage />
        </FormItem>
      )}
    />
  );
};

export default Timezone;

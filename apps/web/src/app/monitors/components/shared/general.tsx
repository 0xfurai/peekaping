import {
  FormControl,
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
import { useFormContext } from "react-hook-form";
import { z } from "zod";
import { useMonitorTranslations } from "@/hooks/useTranslation";

const monitorTypes = [
  {
    type: "http",
    description: "HTTP(S) Monitor",
  },
  {
    type: "tcp",
    description: "TCP Port Monitor",
  },
  {
    type: "ping",
    description: "Ping Monitor (ICMP)",
  },
  {
    type: "dns",
    description: "DNS Monitor",
  },
  {
    type: "push",
    description: "Push Monitor (external service calls a generated URL)",
  },
  {
    type: "docker",
    description: "Docker Container",
  },
  {
    type: "grpc-keyword",
    description: "gRPC Keyword Monitor",
  },
  {
    type: "snmp",
    description: "SNMP Monitor",
  },
];

export const generalDefaultValues = {
  name: "My monitor",
};

export const generalSchema = z.object({
  name: z.string(),
});

const General = () => {
  const form = useFormContext();
  const translations = useMonitorTranslations();

  return (
    <>
      <TypographyH4>General</TypographyH4>
      <FormField
        control={form.control}
        name="name"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{translations.friendlyName}</FormLabel>
            <FormControl>
              <Input placeholder={translations.placeholders.friendlyName} {...field} />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />

      <FormField
        control={form.control}
        name="type"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{translations.monitorType}</FormLabel>
            <Select
              onValueChange={(val) => {
                field.onChange(val);
              }}
              value={field.value}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder={translations.placeholders.monitorType} />
                </SelectTrigger>
              </FormControl>

              <SelectContent>
                {monitorTypes.map((monitor) => (
                  <SelectItem key={monitor.type} value={monitor.type}>
                    {monitor.description}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <FormMessage />
          </FormItem>
        )}
      />
    </>
  );
};

export default General;

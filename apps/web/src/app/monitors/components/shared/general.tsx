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

  return (
    <>
      <TypographyH4>General</TypographyH4>
      <FormField
        control={form.control}
        name="name"
        render={({ field }) => (
          <FormItem>
            <FormLabel>Friendly name</FormLabel>
            <FormControl>
              <Input placeholder="Select friendly name" {...field} />
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
            <FormLabel>Monitor type</FormLabel>
            <Select
              onValueChange={(val) => {
                field.onChange(val);
              }}
              value={field.value}
            >
              <FormControl>
                <SelectTrigger>
                  <SelectValue placeholder="Select monitor type" />
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

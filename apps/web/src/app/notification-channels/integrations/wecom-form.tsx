import { z } from "zod";  
import { useFormContext } from "react-hook-form";  
import { Input } from "@/components/ui/input";  
import {  
  FormField,  
  FormItem,  
  FormLabel,  
  FormControl,  
  FormMessage,  
  FormDescription,  
} from "@/components/ui/form";  
  
export const schema = z.object({  
  type: z.literal("wecom"),  
  webhook_url: z.string().url({ message: "Valid webhook URL is required" }),  
  custom_message: z.string().optional(),  
});  
  
export type WeComFormValues = z.infer<typeof schema>;  
  
export const defaultValues: WeComFormValues = {  
  type: "wecom",  
  webhook_url: "",  
  custom_message: "{{ msg }}",  
};  
  
export const displayName = "WeCom (企业微信)";  
  
export default function WeComForm() {  
  const form = useFormContext();  
    
  return (  
    <>  
      <FormField  
        control={form.control}  
        name="webhook_url"  
        render={({ field }) => (  
          <FormItem>  
            <FormLabel>Webhook URL</FormLabel>  
            <FormControl>  
              <Input  
                placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"  
                type="url"  
                required  
                {...field}  
              />  
            </FormControl>  
            <FormDescription>  
              Wecom's Webhook URL  
            </FormDescription>  
            <FormMessage />  
          </FormItem>  
        )}  
      />  
        
      <FormField  
        control={form.control}  
        name="custom_message"  
        render={({ field }) => (  
          <FormItem>  
            <FormLabel>Custom message</FormLabel>  
            <FormControl>  
              <Input  
                placeholder="{{ msg }}"  
                {...field}  
              />  
            </FormControl>  
            <FormDescription>  
              Custom message template。Available varible: {"{{ msg }}"}, {"{{ name }}"}, {"{{ status }}"}  
            </FormDescription>  
            <FormMessage />  
          </FormItem>  
        )}  
      />  
    </>  
  );  
}

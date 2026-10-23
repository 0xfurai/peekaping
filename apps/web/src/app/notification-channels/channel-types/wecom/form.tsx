import { SecretInput } from "../../components/saved-secrets";
import { useFormContext } from "react-hook-form";
import { Input } from "@/components/ui/input";
import { FormField, FormItem, FormLabel, FormControl, FormMessage, FormDescription } from "@/components/ui/form";

  
  
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
              <SecretInput
                placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"
                autoComplete="off"
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

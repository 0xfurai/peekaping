import { useMonitorFormContext } from "../context/monitor-form-context";
import { getMonitorComponent } from "./monitor-registry";

const CreateEditForm = () => {
  const { form } = useMonitorFormContext();
  const type = form.watch("type");

  const TypeComponent = getMonitorComponent(type);

  if (!TypeComponent) {
    console.log("TypeComponent not found", type);
    return null;
  }

  if (Object.keys(form.formState.errors).length > 0) {
    console.log("CreateEditFields errors", form.formState.errors);
  }

  return <TypeComponent />;
};

export default CreateEditForm;

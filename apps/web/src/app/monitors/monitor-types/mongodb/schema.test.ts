import { describe, expect, it } from "vitest";
import { mongodbDefaultValues, mongodbSchema } from "./schema";

const refuses = (connectionString: string) =>
  mongodbSchema.safeParse({ ...mongodbDefaultValues, connectionString }).success === false;

describe("mongodbSchema connection string", () => {
  it.each([
    ["a single host", "mongodb://username:password@host:27017/database"],
    ["a host with no credentials or port", "mongodb://host/database"],
    ["an SRV name", "mongodb+srv://user:pass@cluster0.example.com/database?retryWrites=true"],
    ["a seed list", "mongodb://h1:27017,h2:27017/db?replicaSet=rs0"],
    ["a seed list with credentials and a host with no port", "mongodb://u:p%40ss@h1:27017,h2:27018,h3/db?replicaSet=rs0"],
    ["a seed list of IPv6 addresses", "mongodb://[fd00::1]:27017,[fd00::2]/db"],
  ])("accepts %s", (_name, connectionString) => {
    expect(refuses(connectionString)).toBe(false);
  });

  it.each([
    ["nothing", ""],
    ["another scheme", "mysql://host:3306/db"],
    ["no scheme", "h1:27017,h2:27017/db"],
    ["no host", "mongodb:///db"],
    ["an empty user name", "mongodb://@host/db"],
    ["a password with no user name", "mongodb://:p@host/db"],
    ["an empty host in a seed list", "mongodb://h1:27017,,h2:27017/db"],
    ["a trailing comma", "mongodb://h1:27017,/db"],
    ["a port that is not a number", "mongodb://h1:27017,h2:abc/db"],
    ["a space in a host", "mongodb://h1:27017, h2:27017/db"],
    ["no database", "mongodb://h1:27017,h2:27017"],
    ["an empty database", "mongodb://h1:27017,h2:27017/"],
  ])("refuses %s", (_name, connectionString) => {
    expect(refuses(connectionString)).toBe(true);
  });
});

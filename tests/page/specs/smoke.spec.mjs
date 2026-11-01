import { test, expect, get, cmd, reported } from "../lib/fixture.mjs";

test("a 4.0 model loads and reports animations", async ({ open }) => {
  await cmd("set-model/v40-solo");
  await open();
  await expect.poll(reported("v40-solo")).toEqual(expect.arrayContaining(["idle", "wave", "spin"]));
});

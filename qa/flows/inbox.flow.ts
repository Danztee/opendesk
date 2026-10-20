import { expect, test } from "../lib/proof.ts";

test("open a ticket from the unassigned queue", async ({ page, proof }) => {
  await proof.open("/inbox/unassigned");
  const tickets = page.getByRole("listbox", { name: "Tickets" });
  await expect(tickets.getByRole("option").first()).toBeVisible();
  await proof.chapter("Inbox", "The unassigned queue, then one ticket beside it");
  await proof.shot("unassigned", { caption: "The unassigned queue" });

  await tickets.getByRole("option", { name: /Pricing question/ }).click();
  await expect(page.getByRole("main").getByText("Is the Business plan billed per member").last()).toBeVisible();
  await proof.dwell();
  await proof.shot("ticket", { caption: "A ticket open beside the queue" });
});

test("an agent edits and deletes an internal note", async ({ page, proof }) => {
  await proof.open("/inbox/unassigned/1086");
  await expect(page.getByRole("heading", { name: "Pricing question" })).toBeVisible();
  await proof.chapter("Add an internal note", "Write a team-only note on a ticket");

  await page.getByRole("radio", { name: "Note" }).click();
  await page.getByRole("textbox", { name: "Note" }).fill("Follow-up on the billing question.");
  await page.getByRole("button", { name: "Add note", exact: true }).click();

  const note = page.getByRole("article", { name: "Note from Maya Chen" });
  await expect(note.getByText("Follow-up on the billing question.", { exact: true })).toBeVisible();
  await note.getByRole("button", { name: "Note actions" }).click();
  await page.getByRole("menuitem", { name: "Edit note" }).click();
  await note.getByRole("textbox", { name: "Edit note" }).fill("Confirmed billing is per workspace.");
  await note.getByRole("button", { name: "Save" }).click();
  await expect(note.getByText("Confirmed billing is per workspace.", { exact: true })).toBeVisible();
  await expect(note.getByText("Edited", { exact: true })).toBeVisible();
  await proof.chapter("Edit the note", "Correct the note and see its edited marker");
  await proof.dwell();
  await proof.shot("note-edited", { target: note, caption: "The corrected note, marked as edited" });

  await note.getByRole("button", { name: "Note actions" }).click();
  await page.getByRole("menuitem", { name: "Delete note" }).click();
  const dialog = page.getByRole("alertdialog");
  await expect(dialog.getByText("This note and its attachments will be permanently deleted.")).toBeVisible();
  await proof.chapter("Delete the note", "Confirm deletion and see the timeline record it");
  await dialog.getByRole("button", { name: "Delete note", exact: true }).click();
  const activity = page.getByText("Maya Chen deleted an internal note", { exact: true });
  await expect(activity).toBeVisible();
  const ollieNotice = page.getByRole("button", { name: "Not now" });
  if (await ollieNotice.isVisible()) await ollieNotice.click();
  await proof.dwell();
  await proof.shot("note-deleted", { caption: "The deletion recorded in the ticket timeline" });
});

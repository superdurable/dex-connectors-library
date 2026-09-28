// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { cloneElement, Fragment, isValidElement, useId, type ButtonHTMLAttributes, type ReactElement, type ReactNode } from "react";

/** StudioSurfaceProps configures the card that frames one Studio surface. */
export interface StudioSurfaceProps {
  /** label names the surface for assistive technology. */
  label: string;
  children: ReactNode;
}

/** StudioSurface is the Dex Web card that frames a connection or configuration unit surface. */
export function StudioSurface({label, children}: StudioSurfaceProps): ReactElement {
  return <section aria-label={label} className="studio-surface">{children}</section>;
}

/** StudioHeaderProps configures a surface heading. */
export interface StudioHeaderProps {
  title: string;
  description?: string;
  /** iconUrl is a bundle-relative image, such as "./icon.svg". */
  iconUrl?: string;
}

/** StudioHeader renders a surface title with an optional connector icon and description. */
export function StudioHeader({title, description, iconUrl}: StudioHeaderProps): ReactElement {
  return <header className="studio-header">
    {iconUrl && <img alt="" src={iconUrl}/>}
    <div><h2>{title}</h2>{description && <p>{description}</p>}</div>
  </header>;
}

/** StudioFieldProps configures a labelled form control. */
export interface StudioFieldProps {
  label: string;
  hint?: string;
  children: ReactNode;
}

/**
 * StudioField labels one form control, with an optional hint below it. The
 * hint sits outside the label, so it is not part of the control's accessible
 * name; when children is a single element, StudioField adds the hint's ID to
 * that element's aria-describedby.
 */
export function StudioField({label, hint, children}: StudioFieldProps): ReactElement {
  const hintId = useId();
  const control = hint && isValidElement<{"aria-describedby"?: string}>(children) && children.type !== Fragment
    ? cloneElement(children, {"aria-describedby": [children.props["aria-describedby"], hintId].filter(Boolean).join(" ")})
    : children;
  return <div className="studio-field">
    <label><span>{label}</span>{control}</label>
    {hint && <small id={hintId}>{hint}</small>}
  </div>;
}

/** StudioButtonProps configures a Dex Web button; primary uses the CTA green. */
export interface StudioButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "secondary";
}

/** StudioButton renders a Dex Web primary or secondary button. It defaults to type="button". */
export function StudioButton({variant = "secondary", className, type = "button", ...props}: StudioButtonProps): ReactElement {
  const classes = ["studio-button", variant === "primary" ? "studio-button-primary" : "", className ?? ""].filter(Boolean).join(" ");
  return <button className={classes} type={type} {...props}/>;
}

/** StudioNoticeProps configures an inline message. */
export interface StudioNoticeProps {
  tone: "info" | "success" | "error" | "attention";
  children: ReactNode;
}

/** StudioNotice renders an inline message; errors use role="alert" and the rest role="status". */
export function StudioNotice({tone, children}: StudioNoticeProps): ReactElement {
  return <p className={`studio-notice studio-notice-${tone}`} role={tone === "error" ? "alert" : "status"}>{children}</p>;
}

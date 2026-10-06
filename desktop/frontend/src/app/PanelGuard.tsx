import { Component, type ReactNode } from "react";
import { t } from "~/lib/i18n";

interface Props {
  // a new key (another space) gives the panel a fresh try; the key must not
  // follow the open chat, or a chat being created would remount its panel
  children: ReactNode;
}

// PanelGuard keeps a fault in one panel from blanking the whole app: what
// failed is replaced by a note with a way to try again, and the title bar
// and the side list stay usable around it.
export class PanelGuard extends Component<Props, { error: Error | null }> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  componentDidCatch(error: Error) {
    console.error("panel failed:", error);
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div className="panel center-panel">
        <div className="space-empty panel-guard" role="alert">
          <strong>{t("panelFailed")}</strong>
          <p>{this.state.error.message}</p>
          <button type="button" className="primary" onClick={() => this.setState({ error: null })}>{t("panelRetry")}</button>
        </div>
      </div>
    );
  }
}

{
					__sveltekit_1oo58s9 = {
						base: ""
					};

					const element = document.currentScript.parentElement;

					Promise.all([
						import("/_app/immutable/entry/start.Cbu43Ltm.js"),
						import("/_app/immutable/entry/app.DfwPaZ5E.js")
					]).then(([kit, app]) => {
						kit.start(app, element);
					});
				}
			
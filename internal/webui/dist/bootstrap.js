{
					__sveltekit_1oo58s9 = {
						base: ""
					};

					const element = document.currentScript.parentElement;

					Promise.all([
						import("/_app/immutable/entry/start.BhQdeEVy.js"),
						import("/_app/immutable/entry/app.DgV3HwGb.js")
					]).then(([kit, app]) => {
						kit.start(app, element);
					});
				}
			